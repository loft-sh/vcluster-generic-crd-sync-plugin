package issuers

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *issuerSyncer) translate(vObj client.Object) *certmanagerv1.Issuer {
	pObj := s.TranslateMetadata(vObj).(*certmanagerv1.Issuer)
	vIssuer := vObj.(*certmanagerv1.Issuer)
	pObj.Spec = *rewriteSpec(&vIssuer.Spec, vIssuer.Namespace)
	return pObj
}

func (s *issuerSyncer) translateUpdate(pObj, vObj *certmanagerv1.Issuer) *certmanagerv1.Issuer {
	var updated *certmanagerv1.Issuer

	// check annotations & labels
	changed, updatedAnnotations, updatedLabels := s.TranslateMetadataUpdate(vObj, pObj)
	if changed {
		updated = newIfNil(updated, pObj)
		updated.Labels = updatedLabels
		updated.Annotations = updatedAnnotations
	}

	// update secret name if necessary
	pSpec := rewriteSpec(&vObj.Spec, vObj.GetNamespace())
	if !equality.Semantic.DeepEqual(*pSpec, pObj.Spec) {
		updated = newIfNil(updated, pObj)
		updated.Spec = *pSpec
	}

	return updated
}

func rewriteSpec(vObjSpec *certmanagerv1.IssuerSpec, namespace string) *certmanagerv1.IssuerSpec {
	// translate secret names
	vObjSpec = vObjSpec.DeepCopy()
	if vObjSpec.ACME != nil {
		vObjSpec.ACME.PrivateKey.Name = translate.PhysicalName(vObjSpec.ACME.PrivateKey.Name, namespace)
	}
	if vObjSpec.CA != nil {
		vObjSpec.CA.SecretName = translate.PhysicalName(vObjSpec.CA.SecretName, namespace)
	}
	if vObjSpec.Vault != nil && vObjSpec.Vault.Auth.TokenSecretRef != nil {
		vObjSpec.Vault.Auth.TokenSecretRef.Name = translate.PhysicalName(vObjSpec.Vault.Auth.TokenSecretRef.Name, namespace)
	}
	if vObjSpec.Venafi != nil && vObjSpec.Venafi.TPP != nil {
		vObjSpec.Venafi.TPP.CredentialsRef.Name = translate.PhysicalName(vObjSpec.Venafi.TPP.CredentialsRef.Name, namespace)
	}
	if vObjSpec.Venafi != nil && vObjSpec.Venafi.Cloud != nil {
		vObjSpec.Venafi.Cloud.APITokenSecretRef.Name = translate.PhysicalName(vObjSpec.Venafi.Cloud.APITokenSecretRef.Name, namespace)
	}
	return vObjSpec
}

func newIfNil(updated *certmanagerv1.Issuer, pObj *certmanagerv1.Issuer) *certmanagerv1.Issuer {
	if updated == nil {
		return pObj.DeepCopy()
	}
	return updated
}
