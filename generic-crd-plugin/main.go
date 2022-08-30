package main

import (
	"os"

	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/namecache"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/syncer"
	"github.com/loft-sh/vcluster-sdk/plugin"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
)

const (
	ConfigurationEnvVar = "CONFIG"
)

func main() {
	// init plugin
	registerCtx, err := plugin.Init()
	if err != nil {
		klog.Fatalf("Error initializing plugin: %v", err)
	}

	c := os.Getenv(ConfigurationEnvVar)
	if c == "" {
		klog.Warning("The %s environment variable is empty, no configuration has been loaded", ConfigurationEnvVar)
	} else {
		klog.Infof("Loading configuration:\n%s", c) //dev
		configuration, err := config.ParseConfig(c)
		if err != nil {
			klog.Fatal(err)
		}

		// create a single name cache
		nc, err := namecache.NewNameCache(registerCtx.Context, registerCtx.VirtualManager, configuration)
		if err != nil {
			klog.Fatalf("Error seting up namecache for a mapping", err)
		}

		// TODO: efficiently sync all mapped CRDs from the host to vcluster or perhaps this should be a separate controller that will watch CRDs and sync changes
		for _, m := range configuration.Mappings {
			if m.FromVirtualCluster != nil {
				if !plugin.Scheme.Recognizes(schema.FromAPIVersionAndKind(m.FromVirtualCluster.ApiVersion, m.FromVirtualCluster.Kind)) {
					err := translate.EnsureCRDFromPhysicalCluster(registerCtx.Context, registerCtx.PhysicalManager.GetConfig(), registerCtx.VirtualManager.GetConfig(), schema.FromAPIVersionAndKind(m.FromVirtualCluster.ApiVersion, m.FromVirtualCluster.Kind))
					if err != nil {
						klog.Fatalf("Error syncronizing CRD %s(%s) from the host cluster into vcluster: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
					}
				}

				s, err := syncer.CreateFromVirtualSyncer(registerCtx, m.FromVirtualCluster, nc)
				if err != nil {
					klog.Fatalf("Error creating %s(%s) syncer: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
				}

				err = plugin.Register(s)
				if err != nil {
					klog.Fatalf("Error registering %s(%s) syncer: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
				}

				for _, c := range m.FromVirtualCluster.SyncBack {
					if !plugin.Scheme.Recognizes(schema.FromAPIVersionAndKind(m.FromVirtualCluster.ApiVersion, m.FromVirtualCluster.Kind)) {
						err := translate.EnsureCRDFromPhysicalCluster(registerCtx.Context, registerCtx.PhysicalManager.GetConfig(), registerCtx.VirtualManager.GetConfig(), schema.FromAPIVersionAndKind(c.ApiVersion, c.Kind))
						if err != nil {
							klog.Fatalf("Error syncronizing CRD %s(%s) from the host cluster into vcluster: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
						}
					}

					backSyncer, err := syncer.CreateBackSyncer(registerCtx, c, m.FromVirtualCluster, nc)
					if err != nil {
						klog.Fatalf("Error creating %s(%s) backsyncer: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
					}

					err = plugin.Register(backSyncer)
					if err != nil {
						klog.Fatalf("Error registering %s(%s) syncer: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
					}
				}
			}
		}
	}

	// start plugin
	err = plugin.Start()
	if err != nil {
		klog.Fatalf("Error starting plugin: %v", err)
	}
}
