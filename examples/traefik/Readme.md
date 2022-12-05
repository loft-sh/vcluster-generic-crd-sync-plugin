# Generic CRD Sync Plugin for Traefik resources

This document covers how to configure generic-crd-sync-plugin to sync traefik resources such as ingressroutes and middlewares.

Prerequisite for installation are the Prometheus CRDs installed in the host cluster where vcluster will be installed.

To install this plugin you need to pass the ./plugin.yaml file as additional source of Helm values during the installation. 
This is described in more detail in vcluster docs - https://www.vcluster.com/docs/plugins/overview#loading-and-installing-plugins-to-vcluster

Example installation command for testing purpose:

vcluster create -n vcluster vcluster -f plugin.yaml

Once installed, the plugin will copy the CRDs from the host cluster into vcluster based on the configuration. Currently the configuration includes sync mappings only for the ingressroutes and middlewares CRD.
