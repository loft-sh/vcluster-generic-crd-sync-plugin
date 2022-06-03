package certificates

import (
	"context"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/constants"
	"github.com/loft-sh/vcluster-sdk/clienthelper"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *certificateSyncer) translate(vObj client.Object) *certmanagerv1.Certificate {
	pObj := s.TranslateMetadata(vObj).(*certmanagerv1.Certificate)
	vCertificate := vObj.(*certmanagerv1.Certificate)
	pObj.Spec = *rewriteSpec(&vCertificate.Spec, vCertificate.Namespace)
	return pObj
}

func (s *certificateSyncer) translateUpdate(pObj, vObj *certmanagerv1.Certificate) *certmanagerv1.Certificate {
	var updated *certmanagerv1.Certificate

	// check annotations & labels
	changed, updatedAnnotations, updatedLabels := s.TranslateMetadataUpdate(vObj, pObj)
	if changed {
		updated = newIfNil(updated, pObj)
		updated.Labels = updatedLabels
		updated.Annotations = updatedAnnotations
	}

	// update spec
	pSpec := rewriteSpec(&vObj.Spec, vObj.GetNamespace())
	if !equality.Semantic.DeepEqual(*pSpec, pObj.Spec) {
		updated = newIfNil(updated, pObj)
		updated.Spec = *pSpec
	}

	return updated
}

func rewriteSpec(vObjSpec *certmanagerv1.CertificateSpec, namespace string) *certmanagerv1.CertificateSpec {
	// translate secret names
	vObjSpec = vObjSpec.DeepCopy()
	if vObjSpec.SecretName != "" {
		vObjSpec.SecretName = translate.PhysicalName(vObjSpec.SecretName, namespace)
	}
	if vObjSpec.IssuerRef.Kind == "Issuer" {
		vObjSpec.IssuerRef.Name = translate.PhysicalName(vObjSpec.IssuerRef.Name, namespace)
	} else if vObjSpec.IssuerRef.Kind == "ClusterIssuer" {
		// TODO: rewrite ClusterIssuers
	}
	if vObjSpec.Keystores != nil && vObjSpec.Keystores.JKS != nil {
		vObjSpec.Keystores.JKS.PasswordSecretRef.Name = translate.PhysicalName(vObjSpec.Keystores.JKS.PasswordSecretRef.Name, namespace)
	}
	if vObjSpec.Keystores != nil && vObjSpec.Keystores.PKCS12 != nil {
		vObjSpec.Keystores.PKCS12.PasswordSecretRef.Name = translate.PhysicalName(vObjSpec.Keystores.PKCS12.PasswordSecretRef.Name, namespace)
	}

	return vObjSpec
}

func (s *certificateSyncer) translateBackwards(pObj *certmanagerv1.Certificate, name types.NamespacedName) (*certmanagerv1.Certificate, error) {
	vCertificate := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name.Name,
			Namespace:   name.Namespace,
			Labels:      pObj.Labels,
			Annotations: map[string]string{},
		},
		Spec: certmanagerv1.CertificateSpec{},
	}
	for k, v := range pObj.Annotations {
		vCertificate.Annotations[k] = v
	}
	vCertificate.Annotations[constants.BackwardSyncAnnotation] = "true"

	// rewrite spec
	vCertificateSpec, err := s.rewriteSpecBackwards(&pObj.Spec, name)
	if err != nil {
		return nil, err
	}

	vCertificate.Spec = *vCertificateSpec
	return vCertificate, nil
}

func (s *certificateSyncer) translateUpdateBackwards(pObj, vObj *certmanagerv1.Certificate) (*certmanagerv1.Certificate, error) {
	var updated *certmanagerv1.Certificate

	// check annotations & labels
	if !equality.Semantic.DeepEqual(pObj.Labels, vObj.Labels) {
		updated = newIfNil(updated, vObj)
		updated.Labels = pObj.Labels
	}

	// check annotations
	newAnnotations := map[string]string{}
	for k, v := range pObj.Annotations {
		newAnnotations[k] = v
	}
	newAnnotations[constants.BackwardSyncAnnotation] = "true"
	if !equality.Semantic.DeepEqual(newAnnotations, vObj.Annotations) {
		updated = newIfNil(updated, vObj)
		updated.Annotations = newAnnotations
	}

	// update spec
	vSpec, err := s.rewriteSpecBackwards(&pObj.Spec, types.NamespacedName{Namespace: vObj.Namespace, Name: vObj.Name})
	if err != nil {
		return nil, err
	}
	if !equality.Semantic.DeepEqual(*vSpec, vObj.Spec) {
		updated = newIfNil(updated, vObj)
		updated.Spec = *vSpec
	}

	return updated, nil
}

func (s *certificateSyncer) rewriteSpecBackwards(pObjSpec *certmanagerv1.CertificateSpec, vName types.NamespacedName) (*certmanagerv1.CertificateSpec, error) {
	vObjSpec := pObjSpec.DeepCopy()

	// find issuer
	vObjSpec.SecretName = vName.Name
	if vObjSpec.IssuerRef.Kind == "Issuer" {
		// try to find issuer
		issuer := &certmanagerv1.Issuer{}
		err := clienthelper.GetByIndex(context.TODO(), s.virtualClient, issuer, translator.IndexByPhysicalName, vObjSpec.IssuerRef.Name)
		if err == nil {
			vObjSpec.IssuerRef.Name = issuer.Name
		}
	}

	return vObjSpec, nil
}

func newIfNil(updated *certmanagerv1.Certificate, pObj *certmanagerv1.Certificate) *certmanagerv1.Certificate {
	if updated == nil {
		return pObj.DeepCopy()
	}
	return updated
}
