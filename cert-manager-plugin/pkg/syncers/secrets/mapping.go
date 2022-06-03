package secrets

import (
	context2 "context"
	"fmt"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/constants"
	"github.com/loft-sh/vcluster-sdk/clienthelper"
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/translate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
)

var (
	IndexByCertificateSecret = "indexbycertificatesecret"
	IndexByIssuerSecret      = "indexbyissuersecret"
)

var _ syncer.IndicesRegisterer = &secretSyncer{}

func (s *secretSyncer) RegisterIndices(ctx *context.RegisterContext) error {
	err := ctx.VirtualManager.GetFieldIndexer().IndexField(ctx.Context, &certmanagerv1.Certificate{}, IndexByCertificateSecret, func(rawObj client.Object) []string {
		return secretNamesFromCertificate(rawObj.(*certmanagerv1.Certificate))
	})
	if err != nil {
		return err
	}
	err = ctx.VirtualManager.GetFieldIndexer().IndexField(ctx.Context, &certmanagerv1.Issuer{}, IndexByIssuerSecret, func(rawObj client.Object) []string {
		return secretNamesFromIssuer(rawObj.(*certmanagerv1.Issuer))
	})
	if err != nil {
		return err
	}

	return s.NamespacedTranslator.RegisterIndices(ctx)
}

var _ syncer.ControllerModifier = &secretSyncer{}

func (s *secretSyncer) ModifyController(ctx *context.RegisterContext, builder *builder.Builder) (*builder.Builder, error) {
	builder = builder.Watches(&source.Kind{Type: &certmanagerv1.Certificate{}}, handler.EnqueueRequestsFromMapFunc(mapCertificates))
	builder = builder.Watches(&source.Kind{Type: &certmanagerv1.Issuer{}}, handler.EnqueueRequestsFromMapFunc(mapIssuers))
	return builder, nil
}

func (s *secretSyncer) shouldSyncBackwards(pSecret, vSecret *corev1.Secret) (bool, types.NamespacedName) {
	// we sync secrets that were generated from certificates or issuers into the vcluster
	if vSecret != nil && vSecret.Annotations != nil && vSecret.Annotations[constants.BackwardSyncAnnotation] == "true" {
		return true, types.NamespacedName{
			Namespace: vSecret.Namespace,
			Name:      vSecret.Name,
		}
	} else if pSecret == nil {
		return false, types.NamespacedName{}
	}

	name := s.nameByIssuer(pSecret)
	if name.Name != "" {
		return true, name
	}

	name = s.nameByCertificate(pSecret)
	if name.Name != "" {
		return true, name
	}

	return false, types.NamespacedName{}
}

func (s *secretSyncer) shouldSyncForward(ctx *context.SyncContext, vObj runtime.Object) (bool, error) {
	secret, ok := vObj.(*corev1.Secret)
	if !ok || secret == nil {
		return false, fmt.Errorf("%#v is not a secret", vObj)
	}

	certificateList := &certmanagerv1.CertificateList{}
	err := ctx.VirtualClient.List(ctx.Context, certificateList, client.MatchingFields{IndexByCertificateSecret: secret.Namespace + "/" + secret.Name})
	if err != nil {
		return false, err
	} else if meta.LenList(certificateList) > 0 {
		return true, nil
	}

	issuerList := &certmanagerv1.IssuerList{}
	err = ctx.VirtualClient.List(ctx.Context, issuerList, client.MatchingFields{IndexByIssuerSecret: secret.Namespace + "/" + secret.Name})
	if err != nil {
		return false, err
	} else if meta.LenList(issuerList) > 0 {
		return true, nil
	}

	return false, nil
}

func (s *secretSyncer) nameByCertificate(pObj client.Object) types.NamespacedName {
	vCertificate := &certmanagerv1.Certificate{}
	err := clienthelper.GetByIndex(context2.TODO(), s.virtualClient, vCertificate, IndexByCertificateSecret, pObj.GetName())
	if err == nil && vCertificate.Name != "" {
		name := vCertificate.Name
		if vCertificate.Spec.SecretName != "" {
			name = vCertificate.Spec.SecretName
		}

		return types.NamespacedName{
			Name:      name,
			Namespace: vCertificate.Namespace,
		}
	}

	return types.NamespacedName{}
}

func (s *secretSyncer) nameByIssuer(pObj client.Object) types.NamespacedName {
	vIssuer := &certmanagerv1.Issuer{}
	err := clienthelper.GetByIndex(context2.TODO(), s.virtualClient, vIssuer, IndexByIssuerSecret, pObj.GetName())
	if err == nil && vIssuer.Name != "" {
		name := vIssuer.Name
		if vIssuer.Spec.ACME != nil && vIssuer.Spec.ACME.PrivateKey.Name != "" {
			name = vIssuer.Spec.ACME.PrivateKey.Name
		}

		return types.NamespacedName{
			Name:      name,
			Namespace: vIssuer.Namespace,
		}
	}

	return types.NamespacedName{}
}

func (s *secretSyncer) PhysicalToVirtual(pObj client.Object) types.NamespacedName {
	namespacedName := s.NamespacedTranslator.PhysicalToVirtual(pObj)
	if namespacedName.Name != "" {
		return namespacedName
	}

	namespacedName = s.nameByCertificate(pObj)
	if namespacedName.Name != "" {
		return namespacedName
	}

	return s.nameByIssuer(pObj)
}

func secretNamesFromCertificate(certificate *certmanagerv1.Certificate) []string {
	secrets := []string{}
	// Do not include certificate.Spec.SecretName here as this will be handled separately by a different controller
	if certificate.Spec.SecretName != "" {
		secrets = append(secrets, translate.PhysicalName(certificate.Spec.SecretName, certificate.Namespace))
		secrets = append(secrets, certificate.Namespace+"/"+certificate.Spec.SecretName)
	} else {
		secrets = append(secrets, translate.PhysicalName(certificate.Name, certificate.Namespace))
		secrets = append(secrets, certificate.Namespace+"/"+certificate.Name)
	}
	if certificate.Spec.Keystores != nil && certificate.Spec.Keystores.JKS != nil && certificate.Spec.Keystores.JKS.PasswordSecretRef.Name != "" {
		secrets = append(secrets, certificate.Namespace+"/"+certificate.Spec.Keystores.JKS.PasswordSecretRef.Name)
	}
	if certificate.Spec.Keystores != nil && certificate.Spec.Keystores.PKCS12 != nil && certificate.Spec.Keystores.PKCS12.PasswordSecretRef.Name != "" {
		secrets = append(secrets, certificate.Namespace+"/"+certificate.Spec.Keystores.PKCS12.PasswordSecretRef.Name)
	}
	return secrets
}

func mapCertificates(obj client.Object) []reconcile.Request {
	certificate, ok := obj.(*certmanagerv1.Certificate)
	if !ok {
		return nil
	}

	requests := []reconcile.Request{}
	names := secretNamesFromCertificate(certificate)
	for _, name := range names {
		splitted := strings.Split(name, "/")
		if len(splitted) == 2 {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: splitted[0],
					Name:      splitted[1],
				},
			})
		}
	}

	return requests
}

func secretNamesFromIssuer(issuer *certmanagerv1.Issuer) []string {
	secrets := []string{}
	if issuer.Spec.ACME != nil && issuer.Spec.ACME.PrivateKey.Name != "" {
		secrets = append(secrets, translate.PhysicalName(issuer.Spec.ACME.PrivateKey.Name, issuer.Namespace))
		secrets = append(secrets, issuer.Namespace+"/"+issuer.Spec.ACME.PrivateKey.Name)
	} else if issuer.Spec.ACME != nil {
		secrets = append(secrets, translate.PhysicalName(issuer.Name, issuer.Namespace))
		secrets = append(secrets, issuer.Namespace+"/"+issuer.Name)
	}
	if issuer.Spec.CA != nil && issuer.Spec.CA.SecretName != "" {
		secrets = append(secrets, issuer.Namespace+"/"+issuer.Spec.CA.SecretName)
	}
	if issuer.Spec.Vault != nil && issuer.Spec.Vault.Auth.TokenSecretRef != nil && issuer.Spec.Vault.Auth.TokenSecretRef.Name != "" {
		secrets = append(secrets, issuer.Namespace+"/"+issuer.Spec.Vault.Auth.TokenSecretRef.Name)
	}
	if issuer.Spec.Venafi != nil && issuer.Spec.Venafi.TPP != nil && issuer.Spec.Venafi.TPP.CredentialsRef.Name != "" {
		secrets = append(secrets, issuer.Namespace+"/"+issuer.Spec.Venafi.TPP.CredentialsRef.Name)
	}
	if issuer.Spec.Venafi != nil && issuer.Spec.Venafi.Cloud != nil && issuer.Spec.Venafi.Cloud.APITokenSecretRef.Name != "" {
		secrets = append(secrets, issuer.Namespace+"/"+issuer.Spec.Venafi.Cloud.APITokenSecretRef.Name)
	}
	return secrets
}

func mapIssuers(obj client.Object) []reconcile.Request {
	issuer, ok := obj.(*certmanagerv1.Issuer)
	if !ok {
		return nil
	}

	requests := []reconcile.Request{}
	names := secretNamesFromIssuer(issuer)
	for _, name := range names {
		splitted := strings.Split(name, "/")
		if len(splitted) == 2 {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: splitted[0],
					Name:      splitted[1],
				},
			})
		}
	}

	return requests
}
