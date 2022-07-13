package main

import (
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/config"
	"github.com/loft-sh/vcluster-generic-crd-plugin/pkg/syncer"
	"github.com/loft-sh/vcluster-sdk/plugin"
	"github.com/loft-sh/vcluster-sdk/translate"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
)

const (
	PluginName = "generic-crd-plugin"
)

func main() {

	// init plugin
	registerCtx, err := plugin.Init(PluginName)
	if err != nil {
		klog.Fatalf("Error initializing plugin: %v", err)
	}

	//TODO: load config from env var or a file,
	// move testing/example one to testing-values.yaml + add it in valuesFiles in devspace.yaml
	// testing config
	config := &config.Config{
		Version: config.Version,
		Mappings: []config.Mapping{
			{
				FromVirtualCluster: &config.FromVirtualCluster{
					TypeInformation: config.TypeInformation{
						ApiVersion: "cert-manager.io/v1",
						Kind:       "Certificate",
					},
					Patches: []*config.Patch{
						{
							Type: config.PatchTypeRewriteName,
							Path: ".spec.issuerRef.name",
						},
					},
					ReversePatches: []*config.Patch{
						{
							Type:     config.PatchTypeCopyFromOtherObject,
							FromPath: "status",
							Path:     "status",
						},
					},
				},
			},
		},
	}

	// TODO: efficiently sync all mapped CRDs from the host to vcluster
	// or perhaps this should be a separate controller that will watch CRDs and sync changes
	// TODO: PoC Phase 2, sync CRDs for the syncBack resources too

	for _, m := range config.Mappings {
		if m.FromVirtualCluster != nil {
			//TEMPORARY, see a TODO above
			err := translate.EnsureCRDFromPhysicalCluster(registerCtx.Context, registerCtx.PhysicalManager.GetConfig(), registerCtx.VirtualManager.GetConfig(), schema.FromAPIVersionAndKind(m.FromVirtualCluster.ApiVersion, m.FromVirtualCluster.Kind))
			if err != nil {
				klog.Fatalf("Error syncronizing CRD %s(%s) from the host cluster into vcluster: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
			}

			s, err := syncer.CreateFromVirtualSyncer(registerCtx, m.FromVirtualCluster)
			if err != nil {
				klog.Fatalf("Error creating %s(%s) syncer: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
			}

			err = plugin.Register(s)
			if err != nil {
				klog.Fatalf("Error registering %s(%s) syncer: %v", m.FromVirtualCluster.Kind, m.FromVirtualCluster.ApiVersion, err)
			}
		}
	}

	// start plugin
	err = plugin.Start()
	if err != nil {
		klog.Fatalf("Error starting plugin: %v", err)
	}
}
