package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/wI2L/jsondiff"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"strings"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/namecache"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/patches"
	"github.com/loft-sh/vcluster-sdk/syncer"
	synccontext "github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	IndexByVirtualName = "indexbyvirtualname"
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

	//  |
	//  |
	//  |
	//  |
	// \|/
	// TODO: TOP priority - initialize a name cache for the translations done in the syncBack patches and reversePatches
	nc := parentNC

	return &backSyncController{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, config.Kind+"-back-syncer", obj),

		options:             ctx.Options,
		physicalClient:      ctx.PhysicalManager.GetClient(),
		config:              config,
		parentConfig:        parentConfig,
		namecache:           nc,
		parentNamecache:     parentNC,
		statusIsSubresource: statusIsSubresource,
	}, nil
}

type backSyncController struct {
	translator.NamespacedTranslator

	options             *synccontext.VirtualClusterOptions
	physicalClient      client.Client
	config              *config.SyncBack
	parentConfig        *config.FromVirtualCluster
	namecache           namecache.NameCache
	parentNamecache     namecache.NameCache
	statusIsSubresource bool
}

var _ syncer.ControllerModifier = &backSyncController{}

func (b *backSyncController) ModifyController(ctx *synccontext.RegisterContext, builder *builder.Builder) (*builder.Builder, error) {
	// Setup a watch to receive a workqueue reference of the controller
	// workqueue can then be used in the cache hooks to trigger a reconcile
	// of this controller when a new entry for paths used by this syncer is added
	return builder.Watches(b, &handler.EnqueueRequestForObject{}), nil
}

var _ syncer.Starter = &backSyncController{}

func (b *backSyncController) ReconcileEnd() {}

func (b *backSyncController) ReconcileStart(ctx *synccontext.SyncContext, req ctrl.Request) (bool, error) {
	// check that VirtualToPhysical won't return empty name to avoid failing here:
	// https://github.com/loft-sh/vcluster-sdk/blob/d1087161ef718af44d08eb8771f6358fb5624265/syncer/syncer.go#L85
	pNN := b.VirtualToPhysical(req.NamespacedName, nil)
	return pNN.Name == "", nil
}

func (b *backSyncController) SyncDown(ctx *synccontext.SyncContext, vObj client.Object) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (b *backSyncController) Sync(ctx *synccontext.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	ctx.Log.Infof("Sync() for pObj %s / vObj %s/%s", pObj.GetName(), vObj.GetNamespace(), vObj.GetName())

	// TODO: test Sync patches and reversePatches after solving a TODO regarding initialization of the b.namecache
	// Execute patches on virtual object
	updatedVObj := vObj.DeepCopyObject().(client.Object)
	result, err := executeObjectPatch(ctx.Context, ctx.VirtualClient, updatedVObj, func() error {
		err := patches.ApplyPatches(updatedVObj, pObj, b.config.Patches, nil, &hostToVirtualNameResolver{nameCache: b.namecache})
		if err != nil {
			return fmt.Errorf("error applying patches: %v", err)
		}
		return nil
	})
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch virtual %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to patch virtual %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
	}
	if result == controllerutil.OperationResultUpdated || result == controllerutil.OperationResultUpdatedStatus || result == controllerutil.OperationResultUpdatedStatusOnly {
		// a change will trigger reconciliation anyway, and at that point we can make
		// a more accurate updates(reverse patches) to the virtual resource
		return ctrl.Result{}, nil
	}

	// Execute reverse patches on physical object
	_, err = executeObjectPatch(ctx.Context, ctx.PhysicalClient, vObj, func() error {
		err = patches.ApplyPatches(pObj, vObj, b.config.ReversePatches, nil, &virtualToHostNameResolver{namespace: vObj.GetNamespace()})
		if err != nil {
			return fmt.Errorf("error applying patches: %v", err)
		}
		return nil
	})
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch physical %s %s/%s: %v", b.config.Kind, pObj.GetNamespace(), pObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to patch physical %s %s/%s: %v", b.config.Kind, pObj.GetNamespace(), pObj.GetName(), err)
	}

	// ensure that the annotation with virtual name and namespace is present on the physical object
	if !b.containsBackSyncNameAnnotations(pObj) {
		err := b.addAnnotationsToPhysicalObject(ctx, pObj, vObj)
		if err != nil {
			if kerrors.IsInvalid(err) {
				ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch virtual %s %s/%s: %v", b.config.Kind, pObj.GetNamespace(), pObj.GetName(), err)
				// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
				// it doesn't seem to have any negative consequence besides the logged error message
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

var _ syncer.UpSyncer = &backSyncController{}

func (b *backSyncController) SyncUp(ctx *synccontext.SyncContext, pObj client.Object) (ctrl.Result, error) {
	vNN := b.PhysicalToVirtual(pObj)
	newObj := pObj.DeepCopyObject().(client.Object)
	newObj.SetResourceVersion("")
	newObj.SetUID("")
	newObj.SetManagedFields([]metav1.ManagedFieldsEntry{})
	newObj.SetNamespace(vNN.Namespace)
	newObj.SetName(vNN.Name)

	err := patches.ApplyPatches(newObj, newObj, b.config.Patches, nil, &hostToVirtualNameResolver{nameCache: b.namecache})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to apply declared patches to %s %s/%s: %v", b.config.Kind, newObj.GetNamespace(), newObj.GetName(), err)
	}

	ctx.Log.Infof("create virtual %s %s/%s", b.config.Kind, newObj.GetNamespace(), newObj.GetName())
	err = ctx.VirtualClient.Create(ctx.Context, newObj)
	if err != nil {
		ctx.Log.Infof("error syncing %s %s/%s to virtual cluster: %v", b.config.Kind, newObj.GetNamespace(), newObj.GetName(), err)
		b.EventRecorder().Eventf(newObj, "Warning", "SyncError", "Error syncing to virtual cluster: %v", err)
		return ctrl.Result{}, err
	}

	// add annotation with virtual name and namespace on the physical object
	err = b.addAnnotationsToPhysicalObject(ctx, pObj, newObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (b *backSyncController) IsManaged(pObj client.Object) (bool, error) {
	return true, nil
}

func (b *backSyncController) VirtualToPhysical(req types.NamespacedName, _ client.Object) types.NamespacedName {
	// TODO: consider creating a simple local map to cache these translations,
	// it would be populated from the PhysicalToVirtual()

	var n string
	for _, s := range b.config.Selectors {
		if s.Name != nil {
			n = b.parentNamecache.ResolveHostNamePath(req, s.Name.RewrittenPath)
		}

		// TODO: implement other selector types here

		if n != "" {
			return types.NamespacedName{
				Namespace: b.options.TargetNamespace,
				Name:      n,
			}
		}
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
			nn := b.parentNamecache.ResolveNamePath(pObj.GetName(), s.Name.RewrittenPath)
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
			rewrittenPath := namecache.MetadataFieldPath
			if s.Name.RewrittenPath != "" {
				rewrittenPath = s.Name.RewrittenPath
			}

			b.parentNamecache.AddChangeHook(namecache.IndexPhysicalToVirtualNamePath, func(name, key, value string) {
				if name != "" {
					// key is format NAMESPACE/NAME/PATH
					splitted := strings.Split(key, "/")
					path := strings.Join(splitted[2:], "/")
					if path == rewrittenPath {
						q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
							Namespace: splitted[0],
							Name:      splitted[1],
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
	a := obj.GetAnnotations()
	return a != nil && a[translate.MarkerLabel] == b.options.Name && a[translator.NameAnnotation] != "" && a[translator.NamespaceAnnotation] != ""
}

func (b *backSyncController) addAnnotationsToPhysicalObject(ctx *synccontext.SyncContext, pObj, vObj client.Object) error {
	_, err := executeObjectPatch(ctx.Context, ctx.PhysicalClient, pObj, func() error {
		annotations := pObj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[translate.MarkerLabel] = b.options.Name
		annotations[translator.NameAnnotation] = vObj.GetName()
		annotations[translator.NamespaceAnnotation] = vObj.GetNamespace()
		pObj.SetAnnotations(annotations)
		return nil
	})
	return err
}

type MutateFn func() error

func executeObjectPatch(ctx context.Context, c client.Client, obj client.Object, f MutateFn) (controllerutil.OperationResult, error) {
	//TODO: we can simplify this function by a lot, aplly the reversePatches on the vObj, produce the json.Diff
	// and then split the resulting diff into to two - changes to the status + all else
	// Current implementation is based on controllerutil.CreateOrPatch

	var updated, statusUpdated bool
	statusIsSubresource := true // do we need to skip status subresource Patch on the resource that don't have status as subresource?

	// Create a copy of the original object as well as converting that copy to
	// unstructured data.
	before, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj.DeepCopyObject())
	if err != nil {
		return controllerutil.OperationResultNone, err
	}
	beforeWithStatus := make(map[string]interface{})
	for k, v := range before {
		beforeWithStatus[k] = v
	}

	// Attempt to extract the status from the resource for easier comparison later
	beforeStatus, hasBeforeStatus, err := unstructured.NestedFieldCopy(before, "status")
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// If the resource contains a status then remove it from the unstructured
	// copy to avoid unnecessary patching later.
	if hasBeforeStatus && statusIsSubresource {
		unstructured.RemoveNestedField(before, "status")
	}

	// Mutate the original object.
	err = f()
	if err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("failed to apply declared patches to %s %s/%s: %v", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName(), err)
	}

	// Convert the resource to unstructured to compare against our before copy.
	after, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// Attempt to extract the status from the resource for easier comparison later
	afterStatus, hasAfterStatus, err := unstructured.NestedFieldCopy(after, "status")
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// If the resource contains a status then remove it from the unstructured
	// copy to avoid unnecessary patching later.
	if hasAfterStatus && statusIsSubresource {
		unstructured.RemoveNestedField(after, "status")
	}

	if !reflect.DeepEqual(before, after) {
		// Only issue a Patch if the before and after resources (minus status) differ

		patch, err := jsondiff.Compare(before, after)
		if err != nil {
			return controllerutil.OperationResultNone, err
		}
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			return controllerutil.OperationResultNone, err
		}

		err = c.Patch(ctx, obj, client.RawPatch(types.JSONPatchType, patchBytes))
		if err != nil {
			return controllerutil.OperationResultNone, err
		}
		updated = true
	}

	if statusIsSubresource && (hasBeforeStatus || hasAfterStatus) && !reflect.DeepEqual(beforeStatus, afterStatus) {
		// Only issue a Status Patch if the resource has a status and the beforeStatus
		// and afterStatus copies differ
		objectAfterPatch, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			if updated {
				return controllerutil.OperationResultUpdated, err
			} else {
				return controllerutil.OperationResultNone, err
			}
		}
		if err = unstructured.SetNestedField(objectAfterPatch, afterStatus, "status"); err != nil {
			if updated {
				return controllerutil.OperationResultUpdated, err
			} else {
				return controllerutil.OperationResultNone, err
			}
		}
		// If Status was replaced by Patch before, restore patched structure to the obj
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(objectAfterPatch, obj); err != nil {
			if updated {
				return controllerutil.OperationResultUpdated, err
			} else {
				return controllerutil.OperationResultNone, err
			}
		}

		statusPatch, err := jsondiff.Compare(beforeWithStatus, objectAfterPatch)
		if err != nil {
			if updated {
				return controllerutil.OperationResultUpdated, err
			} else {
				return controllerutil.OperationResultNone, err
			}
		}
		statusPatchBytes, err := json.Marshal(statusPatch)
		if err != nil {
			if updated {
				return controllerutil.OperationResultUpdated, err
			} else {
				return controllerutil.OperationResultNone, err
			}
		}

		if err := c.Status().Patch(ctx, obj, client.RawPatch(types.JSONPatchType, statusPatchBytes)); err != nil {
			if updated {
				return controllerutil.OperationResultUpdated, err
			} else {
				return controllerutil.OperationResultNone, err
			}
		}
		statusUpdated = true
	}
	if updated && statusUpdated {
		return controllerutil.OperationResultUpdatedStatus, nil
	} else if updated && !statusUpdated {
		return controllerutil.OperationResultUpdated, nil
	} else if !updated && statusUpdated {
		return controllerutil.OperationResultUpdatedStatusOnly, nil
	} else {
		return controllerutil.OperationResultNone, err
	}
}
