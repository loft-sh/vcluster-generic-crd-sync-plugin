# vcluster Plugins

This is the official vcluster Plugins Repository. Read more in the [vcluster documentation](https://www.vcluster.com/docs/plugins/overview).

All plugins have been vetted and approved by the Loft team.

Plugins:
- [`cert-manager-plugin`](https://github.com/loft-sh/vcluster-plugins/tree/master/cert-manager-plugin): Reuse the host cluster's [cert-manager](https://cert-manager.io/docs/) inside vcluster. Syncs issuers and certificates to vcluster 

- [`deeplay-io/vcluster-contour-sync-plugin`](https://github.com/deeplay-io/vcluster-contour-sync-plugin): This plugin syncs Contour resources from the virtual cluster to the host cluster. It expects that Contour CRDs were already installed in the host cluster.
