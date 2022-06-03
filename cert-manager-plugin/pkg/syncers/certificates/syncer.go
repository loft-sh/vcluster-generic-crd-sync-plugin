package certificates

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/loft-sh/vcluster-sdk/syncer"
	"github.com/loft-sh/vcluster-sdk/syncer/context"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func New(ctx *context.RegisterContext) syncer.Syncer {
	return &certificateSyncer{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, "certificate", &certmanagerv1.Certificate{}),

		virtualClient: ctx.VirtualManager.GetClient(),
	}
}

type certificateSyncer struct {
	translator.NamespacedTranslator

	virtualClient client.Client
}

var _ syncer.Initializer = &certificateSyncer{}

func (s *certificateSyncer) Init(ctx *context.RegisterContext) error {
	return translate.EnsureCRDFromPhysicalCluster(ctx.Context, ctx.PhysicalManager.GetConfig(), ctx.VirtualManager.GetConfig(), certmanagerv1.SchemeGroupVersion.WithKind("Certificate"))
}

func (s *certificateSyncer) SyncDown(ctx *context.SyncContext, vObj client.Object) (ctrl.Result, error) {
	vCertificate := vObj.(*certmanagerv1.Certificate)

	// was certificate created by ingress?
	shouldSync, _ := s.shouldSyncBackwards(nil, vCertificate)
	if shouldSync {
		// delete here as certificate is no longer needed
		ctx.Log.Infof("delete virtual certificate %s/%s, because physical got deleted", vObj.GetNamespace(), vObj.GetName())
		return ctrl.Result{}, ctx.VirtualClient.Delete(ctx.Context, vObj)
	}

	return s.SyncDownCreate(ctx, vObj, s.translate(vCertificate))
}

func (s *certificateSyncer) Sync(ctx *context.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	vCertificate := vObj.(*certmanagerv1.Certificate)
	pCertificate := pObj.(*certmanagerv1.Certificate)

	if !equality.Semantic.DeepEqual(vCertificate.Status, pCertificate.Status) {
		newIssuer := vCertificate.DeepCopy()
		newIssuer.Status = pCertificate.Status
		ctx.Log.Infof("update virtual certificate %s/%s, because status is out of sync", vCertificate.Namespace, vCertificate.Name)
		err := ctx.VirtualClient.Status().Update(ctx.Context, newIssuer)
		if err != nil {
			return ctrl.Result{}, err
		}

		// we will requeue anyways
		return ctrl.Result{}, nil
	}

	// was certificate created by ingress?
	shouldSync, _ := s.shouldSyncBackwards(pCertificate, vCertificate)
	if shouldSync {
		updated, err := s.translateUpdateBackwards(pCertificate, vCertificate)
		if err != nil {
			return ctrl.Result{}, err
		}
		if updated != nil {
			ctx.Log.Infof("update virtual certificate %s/%s, because spec is out of sync", vCertificate.Namespace, vCertificate.Name)
			return ctrl.Result{}, s.virtualClient.Update(ctx.Context, updated)
		}

		return ctrl.Result{}, nil
	}

	// did the certificate change?
	return s.SyncDownUpdate(ctx, vObj, s.translateUpdate(pCertificate, vCertificate))
}

var _ syncer.UpSyncer = &certificateSyncer{}

func (s *certificateSyncer) SyncUp(ctx *context.SyncContext, pObj client.Object) (ctrl.Result, error) {
	pCertificate := pObj.(*certmanagerv1.Certificate)

	// was certificate created by ingress?
	shouldSync, vName := s.shouldSyncBackwards(pCertificate, nil)
	if shouldSync {
		ctx.Log.Infof("create virtual certificate %s/%s, because physical is there and virtual is missing", vName.Namespace, vName.Name)
		vCertificate, err := s.translateBackwards(pCertificate, vName)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, s.virtualClient.Create(ctx.Context, vCertificate)
	}

	managed, err := s.IsManaged(pObj)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !managed {
		return ctrl.Result{}, nil
	}
	return syncer.DeleteObject(ctx, pObj)
}
