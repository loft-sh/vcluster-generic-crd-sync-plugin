package main

import (
	"github.com/loft-sh/vcluster-knative-plugin/pkg/syncers/ksvc"
	"github.com/loft-sh/vcluster-sdk/plugin"
	"k8s.io/klog"
	ksvcv1 "knative.dev/serving/pkg/apis/serving/v1"
)

func init() {
	_ = ksvcv1.AddToScheme(plugin.Scheme)
}

func main() {
	registerCtx, err := plugin.Init("knative-serving-plugin")
	if err != nil {
		klog.Fatalf("Error initializing plugin: %v", err)
	}

	syncer := ksvc.New(registerCtx)
	syncer.Name()

	err = plugin.Register(syncer)
	if err != nil {
		klog.Fatalf("Error registering ksvc syncer: %v", err)
	}

	err = plugin.Start()
	if err != nil {
		klog.Fatalf("Error starting plugin: %v", err)
	}
}
