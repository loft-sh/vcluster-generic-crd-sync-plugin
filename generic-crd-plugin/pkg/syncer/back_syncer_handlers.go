package syncer

import (
	"context"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Create is called in response to an create event - e.g. Pod Creation.
func (b *backSyncController) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	b.enqueueVirtual(evt.Object, q)
}

// Update is called in response to an update event -  e.g. Pod Updated.
func (b *backSyncController) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	b.enqueueVirtual(evt.ObjectNew, q)
}

// Delete is called in response to a delete event - e.g. Pod Deleted.
func (b *backSyncController) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	b.enqueueVirtual(evt.Object, q)
}

// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
func (b *backSyncController) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	b.enqueueVirtual(evt.Object, q)
}

func (b *backSyncController) enqueueVirtual(obj client.Object, q workqueue.RateLimitingInterface) {
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
		b.log.Infof("delete virtual %s/%s, because physical is missing, but virtual object exists", obj.GetNamespace(), obj.GetName())
		err := b.virtualClient.Delete(context.Background(), obj)
		if err != nil && !kerrors.IsNotFound(err) {
			b.log.Errorf("error deleting virtual %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
			return
		}
		return
	}

	pObj := objs[0].(client.Object)
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Namespace: pObj.GetNamespace(),
		Name:      pObj.GetName(),
	}})
}
