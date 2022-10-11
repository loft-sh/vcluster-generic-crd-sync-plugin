# Generic CRD Sync Plugin

This plugin allows reusing host cluster CRDs inside a vcluster. The plugin synchronizes the host cluster CRD into the virtual cluster and syncs created objects within the vcluster back to the host cluster. 

A detailed overview and configuration reference is available here:  
https://www.vcluster.com/docs/plugins/generic-crd-sync


# Configuration examples
In this repo we are also providing a couple of plugin configuration as an example.  
These ones are currently available:
- [cert-manager](./crds/cert-manager/)
- [contour](./crds/contour/)
- [istio](./crds/istio/)
- [prometheus-operator](./crds/prometheus-operator/)


**Note:** The configurations mentioned above are provided without any commercial support.  
**Note:** The configurations mentioned above are covering only subsets of features of a give project.
