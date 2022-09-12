# Generic CRD Sync Plugin

This plugin allows reusing host cluster CRDs inside a vcluster. The plugin synchronizes the host cluster CRD into the virtual cluster and syncs created objects within the vcluster back to the host cluster. 

**This currently only works for namespaced resources. Cluster scoped resource support is planned as well, but not yet implemented.**



