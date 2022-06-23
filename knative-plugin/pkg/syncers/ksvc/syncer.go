package ksvc

import (
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/api/equality"
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

	pKsvc := pObj.(*ksvcv1.Service)
	vKsvc := vObj.(*ksvcv1.Service)

	// sync and update ksvc status upwards
	if !equality.Semantic.DeepEqual(vKsvc.Status, pKsvc.Status) {
		newKsvc := vKsvc.DeepCopy()
		newKsvc.Status = pKsvc.Status
		klog.Infof("Update virtual ksvc %s:%s, because status is out of sync", vKsvc.Namespace, vKsvc.Name)
		err := ctx.VirtualClient.Status().Update(ctx.Context, newKsvc)
		if err != nil {
			klog.Errorf("Error updating virtual ksvc status for %s:%s, %v", vKsvc.Namespace, vKsvc.Name, err)
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// sync and update ksvc spec downwards
	// TODO: this needs more fine grained comparision
	// as some fields would need translation implemented
	if newPksvc := k.translateUpdate(pKsvc, vKsvc); newPksvc != nil {
		klog.Infof("Propagate update down to physical ksvc %s:%s as spec has changed", pKsvc.Namespace, pKsvc.Name)
		err := ctx.PhysicalClient.Update(ctx.Context, newPksvc)
		if err != nil {
			klog.Infof("error updating physical ksvc %s:%s spec, %v", pKsvc.Namespace, pKsvc.Name, err)
			return ctrl.Result{}, err
		}

		klog.Infof("successfully updated physical ksvc %s:%s spec", pKsvc.Namespace, pKsvc.Name)
		return ctrl.Result{}, nil
	}

	updated := k.translateUpdateBackwards(pKsvc, vKsvc)
	if updated != nil {
		ctx.Log.Infof("update virtual ksvc %s:%s because spec is out of sync", vKsvc.Namespace, vKsvc.Namespace)
		return ctrl.Result{}, ctx.VirtualClient.Update(ctx.Context, updated)
	}

	return ctrl.Result{}, nil
}
