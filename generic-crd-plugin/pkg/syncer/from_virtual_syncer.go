package syncer

import (
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	synccontext "github.com/loft-sh/vcluster-sdk/syncer/context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateFromVirtualSyncer(ctx *synccontext.RegisterContext, config *config.FromVirtualCluster) (syncer.Syncer, error) {
	obj := &unstructured.Unstructured{}
	obj.SetKind(config.Kind)
	obj.SetAPIVersion(config.ApiVersion)

	var err error
	var selector labels.Selector
	if config.Selector != nil && len(config.Selector.LabelSelector) > 0 {
		selector, err = metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(labels.Set(f.config.Selector.LabelSelector)))
		if err != nil {
			return nil, errors.Wrap(err, "parse label selector")
		}
	}

	return &fromVirtualController{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, config.Kind+"-from-virtual-syncer", obj),

		config:   config,
		selector: selector,
	}, nil
}

type fromVirtualController struct {
	translator.NamespacedTranslator

	config   *config.FromVirtualCluster
	selector labels.Selector
}

func (f *fromVirtualController) SyncDown(ctx *synccontext.SyncContext, vObj client.Object) (ctrl.Result, error) {
	// check if selector matches
	if !f.objectMatches(vObj) {
		return ctrl.Result{}, nil
	}

	// new obj
	newObj := f.TranslateMetadata(vObj)

	return ctrl.Result{}, nil
}

func (f *fromVirtualController) Sync(ctx *synccontext.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	if !f.objectMatches(vObj) {
		ctx.Log.Infof("delete physical %s %s/%s, because it is not used anymore", f.config.Kind, pObj.GetNamespace(), pObj.GetName())
		err := ctx.PhysicalClient.Delete(ctx.Context, pObj)
		if err != nil {
			ctx.Log.Infof("error deleting physical %s %s/%s in physical cluster: %v", f.config.Kind, pObj.GetNamespace(), pObj.GetName(), err)
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (f *fromVirtualController) objectMatches(obj client.Object) bool {
	return f.selector != nil && !f.selector.Matches(labels.Set(obj.GetLabels()))
}
