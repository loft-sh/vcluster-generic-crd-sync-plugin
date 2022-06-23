package ksvc

import (
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/klog"
	ksvcv1 "knative.dev/serving/pkg/apis/serving/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (k *ksvcSyncer) translate(vObj client.Object) *ksvcv1.Service {
	pObj := k.TranslateMetadata(vObj).(*ksvcv1.Service)
	vKsvc := vObj.(*ksvcv1.Service)

	pObj.Spec = *rewriteSpec(&vKsvc.Spec, vKsvc.Namespace)

	return pObj
}

func (k *ksvcSyncer) translateUpdate(pObj, vObj *ksvcv1.Service) *ksvcv1.Service {
	if equality.Semantic.DeepEqual(pObj, vObj) {
		return nil
	}

	var newPKsvc *ksvcv1.Service

	// check for configuration fields
	newPKsvc = updateConfigurationSpec(newPKsvc, pObj, vObj)

	return newPKsvc
}

func (k *ksvcSyncer) translateUpdateBackwards(pObj, vObj *ksvcv1.Service) *ksvcv1.Service {
	var updated *ksvcv1.Service
	// check annotations and labels
	if !equality.Semantic.DeepEqual(pObj.Labels, vObj.Labels) {
		updated = newIfNil(updated, vObj)
		updated.Labels = pObj.Labels
	}

	// check annotations
	newAnnotations := map[string]string{}
	for k, v := range pObj.Annotations {
		newAnnotations[k] = v
	}

	if !equality.Semantic.DeepEqual(newAnnotations, vObj.Annotations) {
		updated = newIfNil(updated, vObj)
		updated.Annotations = newAnnotations
	}

	// check spec
	if !equality.Semantic.DeepEqual(pObj.Spec.Traffic, vObj.Spec.Traffic) {
		klog.Infof("spec.traffic for vKsvc %s:%s, is out of sync", vObj.Namespace, vObj.Name)
		updated = newIfNil(updated, vObj)
		updated.Spec.Traffic = pObj.Spec.Traffic
	}

	return updated

}

func rewriteSpec(vObjSpec *ksvcv1.ServiceSpec, namespace string) *ksvcv1.ServiceSpec {
	vObjSpec = vObjSpec.DeepCopy()

	klog.Info("template name: ", vObjSpec.ConfigurationSpec.Template.Name)
	if vObjSpec.ConfigurationSpec.Template.Name != "" {
		vObjSpec.ConfigurationSpec.Template.Name = translate.PhysicalName(vObjSpec.ConfigurationSpec.Template.Name, namespace)
	}

	return vObjSpec
}

func newIfNil(updated, obj *ksvcv1.Service) *ksvcv1.Service {
	if updated == nil {
		return obj.DeepCopy()
	}

	return updated
}

func updateConfigurationSpec(newPKsvc, pObj, vObj *ksvcv1.Service) *ksvcv1.Service {
	if !equality.Semantic.DeepEqual(
		vObj.Spec.ConfigurationSpec.Template.Spec.Containers[0].Image,
		pObj.Spec.ConfigurationSpec.Template.Spec.Containers[0].Image) {

		newPKsvc = newIfNil(newPKsvc, pObj)

		klog.Infof("image different for vKsvc %s:%s", vObj.Namespace, vObj.Name)
		newPKsvc.Spec.ConfigurationSpec.Template.Spec.Containers[0].Image = vObj.Spec.ConfigurationSpec.Template.Spec.Containers[0].Image
	}

	return newPKsvc
}
