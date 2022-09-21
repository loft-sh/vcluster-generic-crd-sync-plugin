package syncer

import (
	"context"
	"strings"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/namecache"
	"github.com/loft-sh/vcluster-sdk/log"
	"github.com/loft-sh/vcluster-sdk/syncer"
	synccontext "github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ForceSyncAnnotation = "vcluster.loft.sh/force-sync"
)

type ForceSyncConfig struct {
	Parent config.FromVirtualCluster
	Patch  config.Patch
}

func CreateForceSyncController(ctx *synccontext.RegisterContext, GVK schema.GroupVersionKind, config []ForceSyncConfig, nameCache namecache.NameCache) (syncer.Base, error) {
	return &forceSyncController{
		log:           log.New(GVK.Kind + "-force-sync-controller"),
		GVK:           GVK,
		config:        config,
		nameCache:     nameCache,
		virtualClient: ctx.VirtualManager.GetClient(),
	}, nil
}

type forceSyncController struct {
	log           log.Logger
	GVK           schema.GroupVersionKind
	config        []ForceSyncConfig
	nameCache     namecache.NameCache
	virtualClient client.Client
}

var _ syncer.ControllerStarter = &backSyncController{}

func (f *forceSyncController) Name() string {
	return f.GVK.Kind + "-force-sync-controller"
}

func (f *forceSyncController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(f.GVK)
	err := f.virtualClient.Get(ctx, req.NamespacedName, obj)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	err = nil
	if containsForceSyncNameAnnotation(obj) && !f.shouldSync(obj) {
		err = f.removeAnnotationsFromVirtualObject(ctx, obj)
	} else if f.shouldSync(obj) && !containsForceSyncNameAnnotation(obj) {
		err = f.addAnnotationsToVirtualObject(ctx, obj)
	}

	return ctrl.Result{}, err
}

func (f *forceSyncController) Register(ctx *synccontext.RegisterContext) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(f.GVK)

	controller := ctrl.NewControllerManagedBy(ctx.VirtualManager).
		Named(f.Name()).
		For(obj, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			if object == nil || object.GetDeletionTimestamp() != nil {
				return false
			}
			return f.shouldSync(object) || containsForceSyncNameAnnotation(object)
		}))).
		Watches(f, nil)
	return controller.Complete(f)
}

func (f *forceSyncController) shouldSync(obj client.Object) bool {
	for _, c := range f.config {
		parentGVK := schema.FromAPIVersionAndKind(c.Parent.APIVersion, c.Parent.Kind)
		nn := f.nameCache.ResolveNamePath(parentGVK, translate.PhysicalName(obj.GetName(), obj.GetNamespace()), c.Patch.Path)
		if nn.Name != "" {
			// if this config matches then we don't evaluate other and return
			return true
		}
	}
	return false
}

var _ source.Source = &backSyncController{}

func (f *forceSyncController) Start(ctx context.Context, h handler.EventHandler, q workqueue.RateLimitingInterface, predicates ...predicate.Predicate) error {
	// setup the necessary name cache hooks
	for _, c := range f.config {
		parentGVK := schema.FromAPIVersionAndKind(c.Parent.APIVersion, c.Parent.Kind)
		f.nameCache.AddChangeHook(parentGVK, namecache.IndexPhysicalToVirtualNamePath, func(name, key, value string) {
			if name != "" {
				// value format is HOST_NAME/PATH
				splitted := strings.Split(key, "/")
				path := strings.Join(splitted[1:], "/")

				nn := namecache.StringToNamespacedName(value)
				if path == c.Patch.Path && nn.Name != "" {
					q.Add(reconcile.Request{NamespacedName: nn})
				}
			}
		})
	}
	return nil
}

func containsForceSyncNameAnnotation(obj client.Object) bool {
	if obj == nil {
		return false
	}
	annotations := obj.GetAnnotations()
	return annotations != nil && annotations[ForceSyncAnnotation] == "true"
}

func (f *forceSyncController) removeAnnotationsFromVirtualObject(ctx context.Context, obj client.Object) error {
	originalObject := obj.DeepCopyObject().(client.Object)
	annotations := obj.GetAnnotations()
	delete(annotations, ForceSyncAnnotation)
	obj.SetAnnotations(annotations)

	patch := client.MergeFrom(originalObject)
	patchBytes, err := patch.Data(obj)
	if err != nil {
		return err
	} else if string(patchBytes) == "{}" {
		return nil
	}

	log.NewFromExisting(f.log.Base(), obj.GetName()).Infof("Remove %s annotation from %s", ForceSyncAnnotation, f.GVK.Kind)
	return f.virtualClient.Patch(ctx, obj, patch)
}

func (f *forceSyncController) addAnnotationsToVirtualObject(ctx context.Context, obj client.Object) error {
	originalObject := obj.DeepCopyObject().(client.Object)
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[ForceSyncAnnotation] = "true"
	obj.SetAnnotations(annotations)

	patch := client.MergeFrom(originalObject)
	patchBytes, err := patch.Data(obj)
	if err != nil {
		return err
	} else if string(patchBytes) == "{}" {
		return nil
	}

	log.NewFromExisting(f.log.Base(), obj.GetName()).Infof("Add %s annotation to %s", ForceSyncAnnotation, f.GVK.Kind)
	return f.virtualClient.Patch(ctx, obj, patch)
}
