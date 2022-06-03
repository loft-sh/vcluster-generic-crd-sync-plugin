package issuers

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
	return &issuerSyncer{
		NamespacedTranslator: translator.NewNamespacedTranslator(ctx, "issuer", &certmanagerv1.Issuer{}),
	}
}

type issuerSyncer struct {
	translator.NamespacedTranslator
}

var _ syncer.Initializer = &issuerSyncer{}

func (s *issuerSyncer) Init(ctx *context.RegisterContext) error {
	return translate.EnsureCRDFromPhysicalCluster(ctx.Context, ctx.PhysicalManager.GetConfig(), ctx.VirtualManager.GetConfig(), certmanagerv1.SchemeGroupVersion.WithKind("Issuer"))
}

func (s *issuerSyncer) SyncDown(ctx *context.SyncContext, vObj client.Object) (ctrl.Result, error) {
	return s.SyncDownCreate(ctx, vObj, s.translate(vObj.(*certmanagerv1.Issuer)))
}

func (s *issuerSyncer) Sync(ctx *context.SyncContext, pObj client.Object, vObj client.Object) (ctrl.Result, error) {
	vIssuer := vObj.(*certmanagerv1.Issuer)
	pIssuer := pObj.(*certmanagerv1.Issuer)

	if !equality.Semantic.DeepEqual(vIssuer.Status, pIssuer.Status) {
		newIssuer := vIssuer.DeepCopy()
		newIssuer.Status = pIssuer.Status
		ctx.Log.Infof("update virtual issuer %s/%s, because status is out of sync", vIssuer.Namespace, vIssuer.Name)
		err := ctx.VirtualClient.Status().Update(ctx.Context, newIssuer)
		if err != nil {
			return ctrl.Result{}, err
		}

		// we will requeue anyways
		return ctrl.Result{}, nil
	}

	// did the certificate change?
	return s.SyncDownUpdate(ctx, vObj, s.translateUpdate(pIssuer, vIssuer))
}
