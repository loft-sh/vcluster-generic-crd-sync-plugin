package ingresses

import (
	"context"
	"fmt"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/constants"
	"github.com/loft-sh/vcluster-sdk/hook"
	"github.com/loft-sh/vcluster-sdk/syncer/translator"
	"github.com/loft-sh/vcluster-sdk/translate"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewIngressHook() hook.ClientHook {
	return &ingressHook{}
}

type ingressHook struct{}

func (p *ingressHook) Name() string {
	return "ingress-hook"
}

func (p *ingressHook) Resource() client.Object {
	return &networkingv1.Ingress{}
}

var _ hook.MutateCreatePhysical = &ingressHook{}

func (p *ingressHook) MutateCreatePhysical(ctx context.Context, obj client.Object) (client.Object, error) {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil, fmt.Errorf("object %v is not an ingress", obj)
	}

	mutateIngress(ingress)
	return ingress, nil
}

var _ hook.MutateUpdatePhysical = &ingressHook{}

func (p *ingressHook) MutateUpdatePhysical(ctx context.Context, obj client.Object) (client.Object, error) {
	ingress, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return nil, fmt.Errorf("object %v is not an ingress", obj)
	}

	mutateIngress(ingress)
	return ingress, nil
}

func mutateIngress(ingress *networkingv1.Ingress) {
	if ingress.Annotations != nil && ingress.Annotations[constants.IssuerAnnotation] != "" {
		ingress.Annotations[constants.IssuerAnnotation] = translate.PhysicalName(ingress.Annotations[constants.IssuerAnnotation], ingress.Annotations[translator.NamespaceAnnotation])
	}
}
