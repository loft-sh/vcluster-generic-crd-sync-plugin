# Deprecation Notice
> [!IMPORTANT]  
> This repository is no longer maintained. 
> 
> The generic CRD plugin functionality has moved to the vCluster settings: [fromHost.customResources](https://www.vcluster.com/docs/vcluster/configure/vcluster-yaml/sync/from-host/custom-resources) / [toHost.customResources](https://www.vcluster.com/docs/vcluster/configure/vcluster-yaml/sync/to-host/advanced/custom-resources)

# Generic CRD Sync Plugin

This plugin allows reusing host cluster CRDs inside a vcluster. The plugin synchronizes the host cluster CRD into the virtual cluster and syncs created objects within the vcluster back to the host cluster. 

A detailed overview and configuration reference is available here:  
https://www.vcluster.com/docs/plugins/generic-crd-sync


# Configuration examples
In this repo we are also providing a couple of plugin configuration as an example.  
These ones are currently available:
- [cert-manager](./examples/cert-manager/)
- [contour](./examples/contour/)
- [istio](./examples/istio/)
- [prometheus-operator](./examples/prometheus-operator/)


**Note:** The configurations mentioned above are provided without any commercial support.  
**Note:** The configurations mentioned above are covering only subsets of features of a given project.
