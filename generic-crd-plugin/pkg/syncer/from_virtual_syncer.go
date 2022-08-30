package syncer

import (
	"fmt"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/namecache"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/plugin"
	"github.com/loft-sh/vcluster-sdk/log"
	"github.com/loft-sh/vcluster-sdk/syncer"
	synccontext "github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func CreateFromVirtualSyncer(ctx *synccontext.RegisterContext, config *config.FromVirtualCluster, nc namecache.NameCache) (syncer.Syncer, error) {
	obj := &unstructured.Unstructured{}
	obj.SetKind(config.Kind)
	obj.SetAPIVersion(config.ApiVersion)

	var err error
	var selector labels.Selector
	if config.Selector != nil && len(config.Selector.LabelSelector) > 0 {
		selector, err = metav1.LabelSelectorAsSelector(metav1.SetAsLabelSelector(config.Selector.LabelSelector))
		if err != nil {
			return nil, errors.Wrap(err, "parse label selector")
		}
	}

	statusIsSubresource := true
	// TODO: [low priority] check if config.Kind + config.ApiVersion has status subresource

	return &fromVirtualController{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, config.Kind+"-from-virtual-syncer", obj),
		patcher: &patcher{
			fromClient:          ctx.VirtualManager.GetClient(),
			toClient:            ctx.PhysicalManager.GetClient(),
			statusIsSubresource: statusIsSubresource,
			log:                 log.New(config.Kind + "-from-virtual-syncer"),
		},
		gvk:       schema.FromAPIVersionAndKind(config.ApiVersion, config.Kind),
		config:    config,
		nameCache: nc,
		selector:  selector,
	}, nil
}

type fromVirtualController struct {
	translator.NamespacedTranslator

	patcher *patcher
	gvk     schema.GroupVersionKind

	config    *config.FromVirtualCluster
	nameCache namecache.NameCache
	selector  labels.Selector
}

func (f *fromVirtualController) SyncDown(ctx *synccontext.SyncContext, vObj client.Object) (ctrl.Result, error) {
	// check if selector matches
	if isControlled(vObj) || !f.objectMatches(vObj) {
		return ctrl.Result{}, nil
	}

	// apply object to physical cluster
	ctx.Log.Infof("Create physical %s %s/%s, since it is missing, but virtual object exists", f.config.Kind, vObj.GetNamespace(), vObj.GetName())
	_, err := f.patcher.ApplyPatches(ctx.Context, vObj, nil, f.config.Patches, f.config.ReversePatches, func(vObj client.Object) (client.Object, error) {
		return f.TranslateMetadata(vObj), nil
	}, &virtualToHostNameResolver{namespace: vObj.GetNamespace()})
	if err != nil {
		f.EventRecorder().Eventf(vObj, "Warning", "SyncError", "Error syncing to physical cluster: %v", err)
		return ctrl.Result{}, fmt.Errorf("error applying patches: %v", err)
	}

	return ctrl.Result{}, nil
}
func (f *fromVirtualController) isExcluded(pObj client.Object) bool {
	labels := pObj.GetLabels()
	return labels == nil || labels[controlledByLabel] != f.getControllerID()
}

func (f *fromVirtualController) Sync(ctx *synccontext.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	if isControlled(vObj) || f.isExcluded(pObj) {
		return ctrl.Result{}, nil
	} else if !f.objectMatches(vObj) {
		ctx.Log.Infof("delete physical %s %s/%s, because it is not used anymore", f.config.Kind, pObj.GetNamespace(), pObj.GetName())
		err := ctx.PhysicalClient.Delete(ctx.Context, pObj)
		if err != nil {
			ctx.Log.Infof("error deleting physical %s %s/%s in physical cluster: %v", f.config.Kind, pObj.GetNamespace(), pObj.GetName(), err)
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// apply reverse patches
	result, err := f.patcher.ApplyReversePatches(ctx.Context, vObj, pObj, f.config.ReversePatches, &hostToVirtualNameResolver{nameCache: f.nameCache, gvk: f.gvk})
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch virtual %s %s/%s: %v", f.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}

		f.EventRecorder().Eventf(vObj, "Warning", "SyncError", "Error syncing to virtual cluster: %v", err)
		return ctrl.Result{}, fmt.Errorf("failed to patch virtual %s %s/%s: %v", f.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
	} else if result == controllerutil.OperationResultUpdated || result == controllerutil.OperationResultUpdatedStatus || result == controllerutil.OperationResultUpdatedStatusOnly {
		// a change will trigger reconciliation anyway, and at that point we can make
		// a more accurate updates(reverse patches) to the virtual resource
		return ctrl.Result{}, nil
	}

	// apply patches
	_, err = f.patcher.ApplyPatches(ctx.Context, vObj, pObj, f.config.Patches, f.config.ReversePatches, func(vObj client.Object) (client.Object, error) {
		return f.TranslateMetadata(vObj), nil
	}, &virtualToHostNameResolver{namespace: vObj.GetNamespace()})
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch physical %s %s/%s: %v", f.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}

		f.EventRecorder().Eventf(vObj, "Warning", "SyncError", "Error syncing to physical cluster: %v", err)
		return ctrl.Result{}, fmt.Errorf("error applying patches: %v", err)
	}

	return ctrl.Result{}, nil
}

var _ syncer.UpSyncer = &fromVirtualController{}

func (f *fromVirtualController) SyncUp(ctx *synccontext.SyncContext, pObj client.Object) (ctrl.Result, error) {
	if !translate.IsManaged(pObj) || f.isExcluded(pObj) {
		return ctrl.Result{}, nil
	}

	// delete physical object because virtual one is missing
	return syncer.DeleteObject(ctx, pObj)
}

func (f *fromVirtualController) getControllerID() string {
	if f.config.ID != "" {
		return f.config.ID
	}
	return plugin.GetPluginName()
}

// TranslateMetadata converts the virtual object into a physical object
func (f *fromVirtualController) TranslateMetadata(vObj client.Object) client.Object {
	pObj := f.NamespacedTranslator.TranslateMetadata(vObj)
	labels := pObj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[controlledByLabel] = f.getControllerID()
	pObj.SetLabels(labels)
	return pObj
}

func (f *fromVirtualController) IsManaged(pObj client.Object) (bool, error) {
	if !translate.IsManaged(pObj) {
		return false, nil
	}

	return !f.isExcluded(pObj), nil
}

func isControlled(obj client.Object) bool {
	return obj.GetLabels() != nil && obj.GetLabels()[controlledByLabel] != ""
}

func (f *fromVirtualController) objectMatches(obj client.Object) bool {
	return f.selector == nil || !f.selector.Matches(labels.Set(obj.GetLabels()))
}

type virtualToHostNameResolver struct {
	namespace string
}

func (r *virtualToHostNameResolver) TranslateName(name string, _ string) (string, error) {
	return translate.PhysicalName(name, r.namespace), nil
}

type hostToVirtualNameResolver struct {
	gvk schema.GroupVersionKind

	nameCache namecache.NameCache
}

func (r *hostToVirtualNameResolver) TranslateName(name string, path string) (string, error) {
	var n types.NamespacedName
	if path == "" {
		n = r.nameCache.ResolveName(r.gvk, name)
	} else {
		n = r.nameCache.ResolveNamePath(r.gvk, name, path)
	}
	if n.Name == "" {
		return "", fmt.Errorf("could not translate %s host resource name to vcluster resource name", name)
	}

	return n.Name, nil
}
