package certificates

import (
	context2 "context"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/constants"
	"github.com/loft-sh/vcluster-sdk/clienthelper"
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/translate"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
)

var (
	IndexByIngressCertificate = "indexbyingresscertificate"
)

var _ syncer.IndicesRegisterer = &certificateSyncer{}

func (s *certificateSyncer) RegisterIndices(ctx *context.RegisterContext) error {
	err := ctx.VirtualManager.GetFieldIndexer().IndexField(ctx.Context, &networkingv1.Ingress{}, IndexByIngressCertificate, func(rawObj client.Object) []string {
		return certificateNamesFromIngress(rawObj.(*networkingv1.Ingress))
	})
	if err != nil {
		return err
	}

	return s.NamespacedTranslator.RegisterIndices(ctx)
}

var _ syncer.ControllerModifier = &certificateSyncer{}

func (s *certificateSyncer) ModifyController(ctx *context.RegisterContext, builder *builder.Builder) (*builder.Builder, error) {
	builder = builder.Watches(&source.Kind{Type: &networkingv1.Ingress{}}, handler.EnqueueRequestsFromMapFunc(mapIngresses))
	return builder, nil
}

func (s *certificateSyncer) shouldSyncBackwards(pCertificate, vCertificate *certmanagerv1.Certificate) (bool, types.NamespacedName) {
	// we sync secrets that were generated from certificates or issuers into the vcluster
	if vCertificate != nil && vCertificate.Annotations != nil && vCertificate.Annotations[constants.BackwardSyncAnnotation] == "true" {
		return true, types.NamespacedName{
			Namespace: vCertificate.Namespace,
			Name:      vCertificate.Name,
		}
	} else if pCertificate == nil {
		return false, types.NamespacedName{}
	}

	name := s.nameByIngress(pCertificate)
	if name.Name != "" {
		return true, name
	}

	return false, types.NamespacedName{}
}

func (s *certificateSyncer) nameByIngress(pObj client.Object) types.NamespacedName {
	vIngress := &networkingv1.Ingress{}
	err := clienthelper.GetByIndex(context2.TODO(), s.virtualClient, vIngress, IndexByIngressCertificate, pObj.GetName())
	if err == nil && vIngress.Name != "" {
		for _, secret := range vIngress.Spec.TLS {
			if translate.PhysicalName(secret.SecretName, vIngress.Namespace) == pObj.GetName() {
				return types.NamespacedName{
					Name:      secret.SecretName,
					Namespace: vIngress.Namespace,
				}
			}
		}
	}

	return types.NamespacedName{}
}

func (s *certificateSyncer) PhysicalToVirtual(pObj client.Object) types.NamespacedName {
	namespacedName := s.NamespacedTranslator.PhysicalToVirtual(pObj)
	if namespacedName.Name != "" {
		return namespacedName
	}

	namespacedName = s.nameByIngress(pObj)
	if namespacedName.Name != "" {
		return namespacedName
	}

	return types.NamespacedName{}
}

func certificateNamesFromIngress(ingress *networkingv1.Ingress) []string {
	certificates := []string{}

	// Do not include certificate.Spec.SecretName here as this will be handled separately by a different controller
	if ingress.Annotations != nil && (ingress.Annotations[constants.IssuerAnnotation] != "" || ingress.Annotations[constants.ClusterIssuerAnnotation] != "") {
		for _, secret := range ingress.Spec.TLS {
			if secret.SecretName == "" {
				continue
			}

			certificates = append(certificates, translate.PhysicalName(secret.SecretName, ingress.Namespace))
			certificates = append(certificates, ingress.Namespace+"/"+secret.SecretName)
		}
	}
	return certificates
}

func mapIngresses(obj client.Object) []reconcile.Request {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil
	}

	requests := []reconcile.Request{}
	names := certificateNamesFromIngress(ingress)
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
