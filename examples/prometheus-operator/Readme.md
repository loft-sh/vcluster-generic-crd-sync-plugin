# How to use vcluster-generic-crd-sync-plugin to sync Prometheus Operator resources
**Note**: Currently this plugin is still underdevelopment and is provided without any commercial support. You are encouraged to report problems via Issues in this repo. For the help with troubleshooting you can post to the "vcluster" channel in our Slack - https://slack.loft.sh/  
**Note**: The configuration provided here is an example. Please review and update it to match your environment and use case.  
**Warning**: namespace selectors are currently not supported, so with the provided configuration the `.spec.namespaceSelector` field is ignored and Services from all vcluster namespaces are selected. You can upvote [issue #15](https://github.com/loft-sh/vcluster-generic-crd-sync-plugin/issues/15) if the namespace selectors are critical for your use cases.

Prerequisite for installation are the Prometheus CRDs installed in the host cluster where vcluster will be installed.


To install this plugin you need to pass the ./plugin.yaml file as additional source of Helm values during the installation. This is described in more detail in vcluster docs - https://www.vcluster.com/docs/plugins/overview#loading-and-installing-plugins-to-vcluster  
For testing purposes you can refer to the plugin.yaml for the Prometheus Operator resources like so: `https://raw.githubusercontent.com/loft-sh/vcluster-generic-crd-sync-plugin/main/crds/prometheus-operator/plugin.yaml`.  
Example installation command:
```
vcluster create -n vcluster vcluster -f https://raw.githubusercontent.com/loft-sh/vcluster-generic-crd-sync-plugin/main/crds/prometheus-operator/plugin.yaml
```


Once installed, the plugin will copy the CRDs from the host cluster into vcluster based on the configuration. Currently the configuration includes sync mappings only for the ServiceMonitor CRD. At this time the `.spec.namespaceSelector` of the ServiceMonitors created in the vcluster is ignored, all namespaces are selected.


**Note:** Configuration provided in the ./plugin.yaml includes some parts that will likely need to be modified by the user. These parts are marked with yaml comments (E.g. `# User TODO:`) and provide further instructions. 