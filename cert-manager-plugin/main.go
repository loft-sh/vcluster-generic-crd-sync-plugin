package main

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/constants"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/hooks/ingresses"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/syncers/certificates"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/syncers/issuers"
	"github.com/loft-sh/vcluster-cert-manager-plugin/pkg/syncers/secrets"
	"github.com/loft-sh/vcluster-sdk/plugin"
	"k8s.io/klog"
)

func init() {
	// Add cert manager types to our plugin scheme
	_ = certmanagerv1.AddToScheme(plugin.Scheme)
}

func main() {
	// init plugin
	registerCtx, err := plugin.Init(constants.PluginName)
	if err != nil {
		klog.Fatalf("Error initializing plugin: %v", err)
	}

	// register ingress hook
	err = plugin.Register(ingresses.NewIngressHook())
	if err != nil {
		klog.Fatalf("Error registering ingress hook: %v", err)
	}

	// register certificate syncer
	err = plugin.Register(certificates.New(registerCtx))
	if err != nil {
		klog.Fatalf("Error registering certificate syncer: %v", err)
	}

	// register issuer syncer
	err = plugin.Register(issuers.New(registerCtx))
	if err != nil {
		klog.Fatalf("Error registering certificate syncer: %v", err)
	}

	// register secrets syncer
	err = plugin.Register(secrets.New(registerCtx))
	if err != nil {
		klog.Fatalf("Error registering secrets syncer: %v", err)
	}

	// start plugin
	err = plugin.Start()
	if err != nil {
		klog.Fatalf("Error starting plugin: %v", err)
	}
}
