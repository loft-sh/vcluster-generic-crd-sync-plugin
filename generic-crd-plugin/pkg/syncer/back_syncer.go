package syncer

import (
	"context"
	"fmt"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/plugin"
	"github.com/loft-sh/vcluster-sdk/log"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/namecache"
	"github.com/loft-sh/vcluster-sdk/syncer"
	synccontext "github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func CreateBackSyncer(ctx *synccontext.RegisterContext, config *config.SyncBack, parentConfig *config.FromVirtualCluster, parentNC namecache.NameCache) (syncer.Syncer, error) {
	if len(config.Selectors) == 0 {
		return nil, fmt.Errorf("the syncBack config for %s (%s) is missing Selectors", config.Kind, parentConfig.Kind)
	}

	obj := &unstructured.Unstructured{}
	obj.SetKind(config.Kind)
	obj.SetAPIVersion(config.ApiVersion)

	// TODO: [low priority] check if config.Kind + config.ApiVersion has status subresource
	statusIsSubresource := true
	return &backSyncController{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, config.Kind+"-back-syncer", obj),
		patcher: &patcher{
			fromClient:          ctx.PhysicalManager.GetClient(),
			toClient:            ctx.VirtualManager.GetClient(),
			statusIsSubresource: statusIsSubresource,
			log:                 log.New(config.Kind + "-back-syncer"),
		},

		parentGVK:       schema.FromAPIVersionAndKind(parentConfig.ApiVersion, parentConfig.Kind),
		options:         ctx.Options,
		config:          config,
		parentNameCache: parentNC,
	}, nil
}

type backSyncController struct {
	translator.NamespacedTranslator

	patcher *patcher

	parentGVK       schema.GroupVersionKind
	options         *synccontext.VirtualClusterOptions
	config          *config.SyncBack
	parentNameCache namecache.NameCache
}

var _ syncer.ControllerModifier = &backSyncController{}

func (b *backSyncController) ModifyController(ctx *synccontext.RegisterContext, builder *builder.Builder) (*builder.Builder, error) {
	// Setup a watch to receive a workqueue reference of the controller
	// workqueue can then be used in the cache hooks to trigger a reconcile
	// of this controller when a new entry for paths used by this syncer is added
	return builder.Watches(b, nil), nil
}

var _ syncer.Starter = &backSyncController{}

func (b *backSyncController) ReconcileEnd() {}

func (b *backSyncController) ReconcileStart(ctx *synccontext.SyncContext, req ctrl.Request) (bool, error) {
	// check that VirtualToPhysical won't return empty name to avoid failing here:
	// https://github.com/loft-sh/vcluster-sdk/blob/d1087161ef718af44d08eb8771f6358fb5624265/syncer/syncer.go#L85
	pNN := b.VirtualToPhysical(req.NamespacedName, nil)
	if pNN.Name == "" {
		// make sure we delete the virtual object if it exists and is managed by us
		vObj := b.Resource()
		err := ctx.VirtualClient.Get(ctx.Context, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, vObj)
		if err == nil {
			_, err := b.SyncDown(ctx, vObj)
			return true, err
		}

		return true, nil
	}
	return false, nil
}

func (b *backSyncController) getControllerID() string {
	if b.config.ID != "" {
		return b.config.ID
	}
	return plugin.GetPluginName()
}

func (b *backSyncController) RegisterIndices(ctx *synccontext.RegisterContext) error {
	// Don't register any indices as we don't need them anyways
	return nil
}

func (b *backSyncController) isExcluded(vObj client.Object) bool {
	labels := vObj.GetLabels()
	return labels == nil || labels[controlledByLabel] != b.getControllerID()
}

func (b *backSyncController) SyncDown(ctx *synccontext.SyncContext, vObj client.Object) (ctrl.Result, error) {
	if b.isExcluded(vObj) {
		return ctrl.Result{}, nil
	}

	ctx.Log.Infof("delete virtual %s/%s, because physical is missing, but virtual object exists", vObj.GetNamespace(), vObj.GetName())
	err := ctx.VirtualClient.Delete(ctx.Context, vObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (b *backSyncController) Sync(ctx *synccontext.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	if b.isExcluded(vObj) {
		return ctrl.Result{}, nil
	}

	// execute reverse patches
	result, err := b.patcher.ApplyReversePatches(ctx.Context, pObj, vObj, b.config.ReversePatches, &virtualToHostNameResolver{namespace: vObj.GetNamespace()})
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch virtual %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}

		b.EventRecorder().Eventf(vObj, "Warning", "SyncError", "Error syncing to physical cluster: %v", err)
		return ctrl.Result{}, fmt.Errorf("failed to patch physical %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
	} else if result == controllerutil.OperationResultUpdated || result == controllerutil.OperationResultUpdatedStatus || result == controllerutil.OperationResultUpdatedStatusOnly {
		// a change will trigger reconciliation anyway, and at that point we can make
		// a more accurate updates(reverse patches) to the virtual resource
		return ctrl.Result{}, nil
	}

	// apply patches
	_, err = b.patcher.ApplyPatches(ctx.Context, pObj, vObj, b.config.Patches, b.config.ReversePatches, func(obj client.Object) (client.Object, error) {
		return b.translateMetadata(obj)
	}, &hostToVirtualNameResolver{nameCache: b.parentNameCache, gvk: b.parentGVK})
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch physical %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}

		b.EventRecorder().Eventf(vObj, "Warning", "SyncError", "Error syncing to virtual cluster: %v", err)
		return ctrl.Result{}, fmt.Errorf("error applying patches: %v", err)
	}

	// ensure that the annotation with virtual name and namespace is present on the physical object
	if !b.containsBackSyncNameAnnotations(pObj) {
		err := b.addAnnotationsToPhysicalObject(ctx, pObj, vObj)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

var _ syncer.UpSyncer = &backSyncController{}

func (b *backSyncController) SyncUp(ctx *synccontext.SyncContext, pObj client.Object) (ctrl.Result, error) {
	// apply object to physical cluster
	ctx.Log.Infof("Create virtual %s %s/%s, since it is missing, but physical object exists", b.config.Kind, pObj.GetNamespace(), pObj.GetName())
	vObj, err := b.patcher.ApplyPatches(ctx.Context, pObj, nil, b.config.Patches, b.config.ReversePatches, b.translateMetadata, &hostToVirtualNameResolver{nameCache: b.parentNameCache, gvk: b.parentGVK})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error applying patches: %v", err)
	}

	// add annotation with virtual name and namespace on the physical object
	err = b.addAnnotationsToPhysicalObject(ctx, pObj, vObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// translateMetadata converts the physical object into a virtual object
func (b *backSyncController) translateMetadata(pObj client.Object) (client.Object, error) {
	vNN := b.PhysicalToVirtual(pObj)
	if vNN.Name == "" {
		return nil, fmt.Errorf("couldn't translate %s/%s into virtual object", pObj.GetNamespace(), pObj.GetName())
	}

	newObj := pObj.DeepCopyObject().(client.Object)
	translator.ResetObjectMetadata(newObj)
	newObj.SetNamespace(vNN.Namespace)
	newObj.SetName(vNN.Name)

	// set annotations
	annotations := newObj.GetAnnotations()
	delete(annotations, translate.MarkerLabel)
	delete(annotations, translator.NameAnnotation)
	delete(annotations, translator.NamespaceAnnotation)
	newObj.SetAnnotations(annotations)

	// set labels
	labels := newObj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[controlledByLabel] = b.getControllerID()
	newObj.SetLabels(labels)

	return newObj, nil
}

func (b *backSyncController) IsManaged(pObj client.Object) (bool, error) {
	return true, nil
}

func (b *backSyncController) VirtualToPhysical(req types.NamespacedName, _ client.Object) types.NamespacedName {
	n := ""
	for _, s := range b.config.Selectors {
		if s.Name != nil {
			if s.Name.RewrittenPath != "" {
				n = b.parentNameCache.ResolveHostNamePath(b.parentGVK, req, s.Name.RewrittenPath)
			} else {
				n = b.parentNameCache.ResolveHostName(b.parentGVK, req)
			}
			if n != "" {
				return types.NamespacedName{
					Namespace: b.options.TargetNamespace,
					Name:      n,
				}
			}
		}

		// TODO: implement other selector types here
	}

	return types.NamespacedName{}
}

func (b *backSyncController) PhysicalToVirtual(pObj client.Object) types.NamespacedName {
	if b.containsBackSyncNameAnnotations(pObj) {
		pAnnotations := pObj.GetAnnotations()
		return types.NamespacedName{
			Namespace: pAnnotations[translator.NamespaceAnnotation],
			Name:      pAnnotations[translator.NameAnnotation],
		}
	}

	for _, s := range b.config.Selectors {
		nn := types.NamespacedName{}
		if s.Name != nil {
			if s.Name.RewrittenPath != "" {
				nn = b.parentNameCache.ResolveNamePath(b.parentGVK, pObj.GetName(), s.Name.RewrittenPath)
			} else {
				nn = b.parentNameCache.ResolveName(b.parentGVK, pObj.GetName())
			}
			if nn.Name == "" {
				continue
			}
		}

		// TODO: implement other selector types here
		// if part of a selector does not match then we call `continue` to try different selector
		if nn.Name != "" {
			// if this selector matches then we don't evaluate other and return
			return nn
		}
	}

	return types.NamespacedName{}
}

var _ source.Source = &backSyncController{}

func (b *backSyncController) Start(ctx context.Context, h handler.EventHandler, q workqueue.RateLimitingInterface, predicates ...predicate.Predicate) error {
	// setup the necessary name cache hooks
	for _, s := range b.config.Selectors {
		if s.Name != nil {
			rewrittenPath := ""
			if s.Name.RewrittenPath != "" {
				rewrittenPath = s.Name.RewrittenPath
			}

			b.parentNameCache.AddChangeHook(b.parentGVK, namecache.IndexPhysicalToVirtualNamePath, func(name, key, value string) {
				if name != "" {
					// key is format PHYSICAL_NAME/PATH
					splitted := strings.Split(key, "/")
					path := strings.Join(splitted[2:], "/")
					if rewrittenPath == "" || path == rewrittenPath {
						// value is format NAMESPACE/NAME
						namespaceName := strings.Split(value, "/")
						q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
							Namespace: namespaceName[0],
							Name:      namespaceName[1],
						}})
					}
				}
			})
		}

		// TODO: implement other selector types here
	}
	return nil
}

func (b *backSyncController) containsBackSyncNameAnnotations(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	return annotations != nil && annotations[translate.MarkerLabel] == b.options.Name && annotations[translator.NameAnnotation] != "" && annotations[translator.NamespaceAnnotation] != ""
}

func (b *backSyncController) addAnnotationsToPhysicalObject(ctx *synccontext.SyncContext, pObj, vObj client.Object) error {
	originalObject := pObj.DeepCopyObject().(client.Object)
	annotations := pObj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[translate.MarkerLabel] = b.options.Name
	annotations[translator.NameAnnotation] = vObj.GetName()
	annotations[translator.NamespaceAnnotation] = vObj.GetNamespace()
	pObj.SetAnnotations(annotations)

	patch := client.MergeFrom(originalObject)
	patchBytes, err := patch.Data(pObj)
	if err != nil {
		return err
	} else if string(patchBytes) == "{}" {
		return nil
	}

	ctx.Log.Infof("Patch marker annotations on object")
	return ctx.PhysicalClient.Patch(ctx.Context, pObj, patch)
}
