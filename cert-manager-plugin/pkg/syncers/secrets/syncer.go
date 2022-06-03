package secrets

import (
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/constants"
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func New(ctx *context.RegisterContext) syncer.Base {
	return &secretSyncer{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, "secret", &corev1.Secret{}),

		virtualClient:  ctx.VirtualManager.GetClient(),
		physicalClient: ctx.PhysicalManager.GetClient(),
	}
}

type secretSyncer struct {
	translator.NamespacedTranslator

	virtualClient  client.Client
	physicalClient client.Client
}

func (s *secretSyncer) SyncDown(ctx *context.SyncContext, vObj client.Object) (ctrl.Result, error) {
	vSecret := vObj.(*corev1.Secret)

	// was secret created by certificate or issuer?
	shouldSync, _ := s.shouldSyncBackwards(nil, vSecret)
	if shouldSync {
		// delete here as secret is no longer needed
		ctx.Log.Infof("delete virtual secret %s/%s, because physical got deleted", vObj.GetNamespace(), vObj.GetName())
		return ctrl.Result{}, ctx.VirtualClient.Delete(ctx.Context, vObj)
	}

	// is secret used by an issuer or certificate?
	createNeeded, err := s.shouldSyncForward(ctx, vObj)
	if err != nil {
		return ctrl.Result{}, err
	} else if !createNeeded {
		return ctrl.Result{}, s.removeController(ctx, vSecret)
	}

	// switch controller
	switched, err := s.switchController(ctx, vSecret)
	if err != nil {
		return ctrl.Result{}, err
	} else if switched {
		return ctrl.Result{}, nil
	}

	// create the secret if it's needed
	return s.SyncDownCreate(ctx, vObj, s.translate(vObj.(*corev1.Secret)))
}

func (s *secretSyncer) Sync(ctx *context.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	vSecret := vObj.(*corev1.Secret)
	pSecret := pObj.(*corev1.Secret)

	// was secret created by certificate or issuer?
	shouldSyncBackwards, _ := s.shouldSyncBackwards(pSecret, vSecret)
	if shouldSyncBackwards {
		// delete here as secret is no longer needed
		if equality.Semantic.DeepEqual(pSecret.Data, vSecret.Data) && vSecret.Type == pSecret.Type {
			return ctrl.Result{}, nil
		}

		// update secret if necessary
		vSecret.Data = pSecret.Data
		vSecret.Type = pSecret.Type
		ctx.Log.Infof("update virtual secret %s/%s because physical secret has changed", vSecret.Namespace, vSecret.Name)
		return ctrl.Result{}, ctx.VirtualClient.Delete(ctx.Context, vObj)
	}

	// is secret used by an issuer or certificate?
	used, err := s.shouldSyncForward(ctx, vObj)
	if err != nil {
		return ctrl.Result{}, err
	} else if !used {
		return ctrl.Result{}, s.removeController(ctx, vSecret)
	}

	// switch controller
	switched, err := s.switchController(ctx, vSecret)
	if err != nil {
		return ctrl.Result{}, err
	} else if switched {
		return ctrl.Result{}, nil
	}

	// update secret if necessary
	return s.SyncDownUpdate(ctx, vObj, s.translateUpdate(pObj.(*corev1.Secret), vObj.(*corev1.Secret)))
}

var _ syncer.UpSyncer = &secretSyncer{}

func (s *secretSyncer) SyncUp(ctx *context.SyncContext, pObj client.Object) (ctrl.Result, error) {
	pSecret := pObj.(*corev1.Secret)

	// was secret created by certificate or issuer?
	shouldSyncBackwards, vName := s.shouldSyncBackwards(pSecret, nil)
	if shouldSyncBackwards {
		vSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        vName.Name,
				Namespace:   vName.Namespace,
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			Data: pSecret.Data,
			Type: pSecret.Type,
		}
		for k, v := range pSecret.Annotations {
			vSecret.Annotations[k] = v
		}
		for k, v := range pSecret.Labels {
			vSecret.Labels[k] = v
		}
		vSecret.Annotations[constants.BackwardSyncAnnotation] = "true"
		vSecret.Labels[translate.ControllerLabel] = constants.PluginName
		ctx.Log.Infof("create virtual secret %s/%s because physical secret exists", vSecret.Namespace, vSecret.Name)
		return ctrl.Result{}, ctx.VirtualClient.Create(ctx.Context, vSecret)
	}

	// don't do anything here
	return ctrl.Result{}, nil
}

func (s *secretSyncer) removeController(ctx *context.SyncContext, vSecret *corev1.Secret) error {
	// remove us as owner
	if vSecret.Labels != nil && vSecret.Labels[translate.ControllerLabel] == constants.PluginName {
		delete(vSecret.Labels, translate.ControllerLabel)
		ctx.Log.Infof("update secret %s/%s because we the controlling party, but secret is not needed anymore", vSecret.Namespace, vSecret.Name)
		return ctx.VirtualClient.Update(ctx.Context, vSecret)
	}

	return nil
}

func (s *secretSyncer) switchController(ctx *context.SyncContext, vSecret *corev1.Secret) (bool, error) {
	// check if we own the secret
	if vSecret.Labels == nil || vSecret.Labels[translate.ControllerLabel] == "" {
		if vSecret.Labels == nil {
			vSecret.Labels = map[string]string{}
		}
		vSecret.Labels[translate.ControllerLabel] = constants.PluginName
		ctx.Log.Infof("update secret %s/%s because we are not the controlling party", vSecret.Namespace, vSecret.Name)
		return true, ctx.VirtualClient.Update(ctx.Context, vSecret)
	} else if vSecret.Labels[translate.ControllerLabel] != constants.PluginName {
		return true, nil
	}

	return false, nil
}
