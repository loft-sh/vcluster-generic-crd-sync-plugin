package ksvc

import (
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/klog"
	ksvcv1 "knative.dev/serving/pkg/apis/serving/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func New(ctx *context.RegisterContext) syncer.Syncer {
	return &ksvcSyncer{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, "serving", &ksvcv1.Service{}),
	}
}

type ksvcSyncer struct {
	translator.NamespacedTranslator
}

var _ syncer.Initializer = &ksvcSyncer{}

func (k *ksvcSyncer) Init(ctx *context.RegisterContext) error {
	return translate.EnsureCRDFromPhysicalCluster(ctx.Context,
		ctx.PhysicalManager.GetConfig(),
		ctx.VirtualManager.GetConfig(),
		ksvcv1.SchemeGroupVersion.WithKind("Service"))
}

// SyncDown defines the action that should be taken by the syncer if a virtual cluster object
// exists, but has no corresponding physical cluster object yet. Typically, the physical cluster
// object would get synced down from the virtual cluster to the host cluster in this scenario.
func (k *ksvcSyncer) SyncDown(ctx *context.SyncContext, vObj client.Object) (ctrl.Result, error) {
	klog.Info("SyncDown called for ", vObj.GetName())
	vKsvc := vObj.(*ksvcv1.Service)

	return k.SyncDownCreate(ctx, vObj, k.translate(vKsvc))
}

// Sync defines the action that should be taken by the syncer if a virtual cluster object and
// physical cluster object exist and either one of them has changed. The syncer is expected
// to reconcile in this case without knowledge of which object has actually changed. This
// is needed to avoid race conditions and defining a clear hierarchy what fields should be managed
// by which cluster. For example, for pods you would want to sync down (virtual -> physical)
// spec changes, while you would want to sync up (physical -> virtual) status changes, as those
// would get set only by the physical host cluster.
func (k *ksvcSyncer) Sync(ctx *context.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	klog.Infof("Sync called for %s : %s", pObj.GetName(), vObj.GetName())
	return ctrl.Result{}, nil
}

func (k *ksvcSyncer) translate(vObj client.Object) *ksvcv1.Service {
	pObj := k.TranslateMetadata(vObj).(*ksvcv1.Service)
	vKsvc := vObj.(*ksvcv1.Service)

	pObj.Spec = *rewriteSpec(&vKsvc.Spec, vKsvc.Namespace)

	return pObj
}

func rewriteSpec(vObjSpec *ksvcv1.ServiceSpec, namespace string) *ksvcv1.ServiceSpec {
	vObjSpec = vObjSpec.DeepCopy()

	klog.Info("template name: ", vObjSpec.ConfigurationSpec.Template.Name)
	if vObjSpec.ConfigurationSpec.Template.Name != "" {
		vObjSpec.ConfigurationSpec.Template.Name = translate.PhysicalName(vObjSpec.ConfigurationSpec.Template.Name, namespace)
	}

	return vObjSpec
}
