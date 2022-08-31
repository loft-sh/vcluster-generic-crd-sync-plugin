package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/plugin"
	"github.com/loft-sh/vcluster-sdk/log"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	IndexByVirtualName = "indexbyvirtualname"

	MappingsAnnotation = "vcluster.loft.sh/mappings"
)

func CreateBackSyncer(ctx *synccontext.RegisterContext, config *config.SyncBack, parentConfig *config.FromVirtualCluster, parentNC namecache.NameCache) (syncer.Base, error) {
	if len(config.Selectors) == 0 {
		return nil, fmt.Errorf("the syncBack config for %s (%s) is missing Selectors", config.Kind, parentConfig.Kind)
	}

	obj := &unstructured.Unstructured{}
	obj.SetKind(config.Kind)
	obj.SetAPIVersion(config.ApiVersion)

	// TODO: [low priority] check if config.Kind + config.ApiVersion has status subresource
	statusIsSubresource := true
	return &backSyncController{
		log: log.New(config.Kind + "-back-syncer"),
		patcher: &patcher{
			fromClient:          ctx.PhysicalManager.GetClient(),
			toClient:            ctx.VirtualManager.GetClient(),
			statusIsSubresource: statusIsSubresource,
			log:                 log.New(config.Kind + "-back-syncer"),
		},

		parentGVK:       schema.FromAPIVersionAndKind(parentConfig.ApiVersion, parentConfig.Kind),
		obj:             obj,
		options:         ctx.Options,
		config:          config,
		parentNameCache: parentNC,
		targetNamespace: ctx.TargetNamespace,
		physicalClient:  ctx.PhysicalManager.GetClient(),

		currentNamespace:       ctx.CurrentNamespace,
		currentNamespaceClient: ctx.CurrentNamespaceClient,

		virtualClient: ctx.VirtualManager.GetClient(),
	}, nil
}

type backSyncController struct {
	patcher *patcher

	log log.Logger
	obj client.Object

	parentGVK schema.GroupVersionKind
	options   *synccontext.VirtualClusterOptions
	config    *config.SyncBack

	parentNameCache namecache.NameCache

	targetNamespace string
	physicalClient  client.Client

	currentNamespace       string
	currentNamespaceClient client.Client

	virtualClient client.Client
}

var _ syncer.ControllerStarter = &backSyncController{}

func (b *backSyncController) Name() string {
	return b.config.Kind + "-back-syncer"
}

var _ syncer.IndicesRegisterer = &backSyncController{}

func (b *backSyncController) RegisterIndices(ctx *synccontext.RegisterContext) error {
	return ctx.PhysicalManager.GetCache().IndexField(ctx.Context, b.resource(), IndexByVirtualName, func(object client.Object) []string {
		if b.containsBackSyncNameAnnotations(object) {
			annotations := object.GetAnnotations()
			return []string{annotations[translator.NamespaceAnnotation] + "/" + annotations[translator.NameAnnotation]}
		}

		return []string{}
	})
}

func (b *backSyncController) resource() client.Object {
	return b.obj.DeepCopyObject().(client.Object)
}

func (b *backSyncController) Register(ctx *synccontext.RegisterContext) error {
	maxConcurrentReconciles := 1
	controller := ctrl.NewControllerManagedBy(ctx.PhysicalManager).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		Named(b.Name()).
		Watches(source.NewKindWithCache(b.resource(), ctx.VirtualManager.GetCache()), &handler.Funcs{
			CreateFunc: func(event event.CreateEvent, limitingInterface workqueue.RateLimitingInterface) {
				b.enqueueVirtual(event.Object, limitingInterface, false)
			},
			UpdateFunc: func(event event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {
				b.enqueueVirtual(event.ObjectNew, limitingInterface, false)
			},
			DeleteFunc: func(event event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
				b.enqueueVirtual(event.Object, limitingInterface, true)
			},
			GenericFunc: func(event event.GenericEvent, limitingInterface workqueue.RateLimitingInterface) {
				b.enqueueVirtual(event.Object, limitingInterface, false)
			},
		}).
		Watches(&source.Kind{Type: b.resource()}, &handler.Funcs{
			DeleteFunc: func(event event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
				// delete virtual resource. Would be nicer to have this part of the controller, but
				// it works for now.
				virtualName := b.PhysicalToVirtual(types.NamespacedName{
					Namespace: event.Object.GetNamespace(),
					Name:      event.Object.GetName(),
				}, event.Object)
				if virtualName.String() != "" {
					vObj := b.resource()
					err := b.virtualClient.Get(context.Background(), virtualName, vObj)
					if err == nil {
						err = b.deleteVirtualObject(ctx.Context, vObj, b.log)
						if err != nil {
							b.log.Errorf("error deleting virtual object %s/%s: %v", vObj.GetNamespace(), vObj.GetName(), err)
						}
					}
				}
			},
		}).
		Watches(b, nil).
		For(b.resource())
	return controller.Complete(b)
}

func (b *backSyncController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.NewFromExisting(b.log.Base(), req.Name)
	syncContext := &synccontext.SyncContext{
		Context:                ctx,
		Log:                    log,
		TargetNamespace:        b.targetNamespace,
		PhysicalClient:         b.physicalClient,
		CurrentNamespace:       b.currentNamespace,
		CurrentNamespaceClient: b.currentNamespaceClient,
		VirtualClient:          b.virtualClient,
	}

	// get physical resource
	pObj := b.resource()
	err := b.physicalClient.Get(ctx, req.NamespacedName, pObj)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		pObj = nil
	}

	// get virtual resource
	vNN := b.PhysicalToVirtual(req.NamespacedName, pObj)
	if vNN.Name == "" {
		// we skip early here, we cannot resolve the physical to virtual,
		// which means it either doesn't matches or shouldn't get synced anymore
		return ctrl.Result{}, nil
	}
	vObj := b.resource()
	err = b.virtualClient.Get(ctx, vNN, vObj)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		vObj = nil
	}

	// check what function we should call
	if vObj != nil && pObj == nil {
		return b.syncDown(syncContext, vObj)
	} else if vObj != nil && pObj != nil {
		return b.sync(syncContext, pObj, vObj)
	} else if vObj == nil && pObj != nil {
		return b.syncUp(syncContext, pObj)
	}

	return ctrl.Result{}, nil
}

func (b *backSyncController) getControllerID() string {
	if b.config.ID != "" {
		return b.config.ID
	}
	return plugin.GetPluginName()
}

func (b *backSyncController) isExcluded(vObj client.Object) bool {
	labels := vObj.GetLabels()
	return labels == nil || labels[controlledByLabel] != b.getControllerID()
}

func (b *backSyncController) syncDown(ctx *synccontext.SyncContext, vObj client.Object) (ctrl.Result, error) {
	return ctrl.Result{}, b.deleteVirtualObject(ctx.Context, vObj, ctx.Log)
}

func (b *backSyncController) deleteVirtualObject(ctx context.Context, vObj client.Object, log log.Logger) error {
	if b.isExcluded(vObj) {
		return nil
	}

	log.Infof("delete virtual %s/%s, because physical is missing, but virtual object exists", vObj.GetNamespace(), vObj.GetName())
	err := b.virtualClient.Delete(ctx, vObj)
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (b *backSyncController) sync(ctx *synccontext.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
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

		return ctrl.Result{}, fmt.Errorf("failed to patch physical %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
	} else if result == controllerutil.OperationResultUpdated || result == controllerutil.OperationResultUpdatedStatus || result == controllerutil.OperationResultUpdatedStatusOnly {
		// a change will trigger reconciliation anyway, and at that point we can make
		// a more accurate updates(reverse patches) to the virtual resource
		return ctrl.Result{}, nil
	}

	// apply patches
	pAnnotations := pObj.GetAnnotations()
	mappings := map[string]string{}
	if pAnnotations != nil && pAnnotations[MappingsAnnotation] != "" {
		_ = json.Unmarshal([]byte(pAnnotations[MappingsAnnotation]), &mappings)
	}
	nameResolver := &memorizingHostToVirtualNameResolver{nameCache: b.parentNameCache, gvk: b.parentGVK, mappings: mappings}
	_, err = b.patcher.ApplyPatches(ctx.Context, pObj, vObj, b.config.Patches, b.config.ReversePatches, func(obj client.Object) (client.Object, error) {
		return b.translateMetadata(obj)
	}, nameResolver)
	if err != nil {
		if kerrors.IsInvalid(err) {
			ctx.Log.Infof("Warning: this message could indicate a timing issue with no significant impact, or a bug. Please report this if your resource never reaches the expected state. Error message: failed to patch physical %s %s/%s: %v", b.config.Kind, vObj.GetNamespace(), vObj.GetName(), err)
			// this happens when some field is being removed shortly after being added, which suggest it's a timing issue
			// it doesn't seem to have any negative consequence besides the logged error message
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, fmt.Errorf("error applying patches: %v", err)
	}

	// ensure that the annotation with virtual name and namespace is present on the physical object
	err = b.addAnnotationsToPhysicalObject(ctx, pObj, types.NamespacedName{
		Namespace: vObj.GetNamespace(),
		Name:      vObj.GetName(),
	}, nameResolver.mappings)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (b *backSyncController) syncUp(ctx *synccontext.SyncContext, pObj client.Object) (ctrl.Result, error) {
	// if the annotations are already there we now the object was created before and apparently it was deleted
	// inside the virtual cluster. So we will also delete it inside the host cluster as well.
	if b.containsBackSyncNameAnnotations(pObj) {
		ctx.Log.Infof("Delete physical %s %s/%s, since it was deleted in virtual cluster or is missing there", b.config.Kind, pObj.GetNamespace(), pObj.GetName())
		err := ctx.PhysicalClient.Delete(ctx.Context, pObj)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error deleting object: %v", err)
		}

		return ctrl.Result{}, nil
	}

	// add annotation with virtual name and namespace on the physical object
	vNN := b.PhysicalToVirtual(types.NamespacedName{
		Namespace: pObj.GetNamespace(),
		Name:      pObj.GetName(),
	}, pObj)
	if vNN.Name == "" {
		return ctrl.Result{}, fmt.Errorf("couldn't translate %s/%s into virtual object", pObj.GetNamespace(), pObj.GetName())
	}
	err := b.addAnnotationsToPhysicalObject(ctx, pObj, vNN, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	// apply object to physical cluster
	ctx.Log.Infof("Create virtual %s %s/%s, since it is missing, but physical object exists", b.config.Kind, pObj.GetNamespace(), pObj.GetName())
	nameResolver := &memorizingHostToVirtualNameResolver{nameCache: b.parentNameCache, gvk: b.parentGVK}
	_, err = b.patcher.ApplyPatches(ctx.Context, pObj, nil, b.config.Patches, b.config.ReversePatches, b.translateMetadata, nameResolver)
	if err != nil {
		_ = b.removeAnnotationsFromPhysicalObject(ctx, pObj)
		return ctrl.Result{}, fmt.Errorf("error applying patches: %v", err)
	}

	// update mappings on object
	err = b.addAnnotationsToPhysicalObject(ctx, pObj, vNN, nameResolver.mappings)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// translateMetadata converts the physical object into a virtual object
func (b *backSyncController) translateMetadata(pObj client.Object) (client.Object, error) {
	vNN := b.PhysicalToVirtual(types.NamespacedName{
		Namespace: pObj.GetNamespace(),
		Name:      pObj.GetName(),
	}, pObj)
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
	delete(annotations, MappingsAnnotation)
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

func (b *backSyncController) PhysicalToVirtual(req types.NamespacedName, pObj client.Object) types.NamespacedName {
	if pObj != nil && b.containsBackSyncNameAnnotations(pObj) {
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
				nn = b.parentNameCache.ResolveNamePath(b.parentGVK, req.Name, s.Name.RewrittenPath)
			} else {
				nn = b.parentNameCache.ResolveName(b.parentGVK, req.Name)
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
						q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
							Namespace: b.targetNamespace,
							Name:      splitted[0],
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

func (b *backSyncController) removeAnnotationsFromPhysicalObject(ctx *synccontext.SyncContext, pObj client.Object) error {
	originalObject := pObj.DeepCopyObject().(client.Object)
	annotations := pObj.GetAnnotations()
	delete(annotations, translate.MarkerLabel)
	delete(annotations, translator.NameAnnotation)
	delete(annotations, translator.NamespaceAnnotation)
	delete(annotations, MappingsAnnotation)
	pObj.SetAnnotations(annotations)

	patch := client.MergeFrom(originalObject)
	patchBytes, err := patch.Data(pObj)
	if err != nil {
		return err
	} else if string(patchBytes) == "{}" {
		return nil
	}

	ctx.Log.Infof("Delete marker annotations on object")
	return ctx.PhysicalClient.Patch(ctx.Context, pObj, patch)
}

func (b *backSyncController) addAnnotationsToPhysicalObject(ctx *synccontext.SyncContext, pObj client.Object, vObj types.NamespacedName, mappings map[string]string) error {
	originalObject := pObj.DeepCopyObject().(client.Object)
	annotations := pObj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[translate.MarkerLabel] = b.options.Name
	annotations[translator.NameAnnotation] = vObj.Name
	annotations[translator.NamespaceAnnotation] = vObj.Namespace
	if len(mappings) > 0 {
		out, _ := json.Marshal(mappings)
		annotations[MappingsAnnotation] = string(out)
	} else {
		delete(annotations, MappingsAnnotation)
	}
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

func (b *backSyncController) enqueueVirtual(obj client.Object, q workqueue.RateLimitingInterface, isDelete bool) {
	if obj == nil {
		return
	} else if b.isExcluded(obj) {
		return
	}

	list := &unstructured.UnstructuredList{}
	list.SetKind(b.config.Kind + "List")
	list.SetAPIVersion(b.config.ApiVersion)
	err := b.physicalClient.List(context.Background(), list, client.MatchingFields{IndexByVirtualName: obj.GetNamespace() + "/" + obj.GetName()})
	if err != nil {
		b.log.Errorf("error listing %s for virtual to physical name translation: %v", b.config.Kind, err)
		return
	}

	objs, err := meta.ExtractList(list)
	if err != nil {
		b.log.Errorf("error extracting list %s for virtual to physical name translation: %v", b.config.Kind, err)
		return
	} else if len(objs) == 0 {
		if !isDelete {
			b.log.Infof("delete virtual %s/%s, because physical is missing, but virtual object exists", obj.GetNamespace(), obj.GetName())
			err := b.virtualClient.Delete(context.Background(), obj)
			if err != nil && !kerrors.IsNotFound(err) {
				b.log.Errorf("error deleting virtual %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
				return
			}
		}
		return
	}

	pObj := objs[0].(client.Object)
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: pObj.GetNamespace(),
		Name:      pObj.GetName(),
	}})
}

type memorizingHostToVirtualNameResolver struct {
	gvk       schema.GroupVersionKind
	nameCache namecache.NameCache

	mappings map[string]string
}

func (r *memorizingHostToVirtualNameResolver) TranslateName(name string, path string) (string, error) {
	if r.mappings == nil {
		r.mappings = map[string]string{}
	}

	key := name + "/" + path
	var n types.NamespacedName
	if path == "" {
		n = r.nameCache.ResolveName(r.gvk, name)
	} else {
		n = r.nameCache.ResolveNamePath(r.gvk, name, path)
	}
	if n.Name == "" {
		if r.mappings[key] != "" {
			return r.mappings[key], nil
		}

		return "", fmt.Errorf("could not translate %s host resource name to vcluster resource name", name)
	}
	r.mappings[key] = n.Name
	return n.Name, nil
}
