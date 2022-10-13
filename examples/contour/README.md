# vcluster-contour-plugin

This plugin syncs contour HTTPProxy resources created in the virtual cluster to the host cluster. It allows us to reuse a single contour ingress controller deployed on the host to manage and control the ingress traffic for all vclusters that run this plugin.

## Quickstart

Deploy vcluster with the plugin:
```
vcluster create my-vcluster -n my-vcluster -f crds/contour/plugin.yaml
```

Now wait until vcluster has started and deploy a HTTPProxy into the vcluster:

`http-proxy.yaml`:
```yaml
apiVersion: projectcontour.io/v1
kind: HTTPProxy
metadata:
  name: basic
spec:
  virtualhost:
    fqdn: sampledomain.io
    tls:
      secretName: sname1234
  routes:
    - conditions:
      - prefix: /prefix
      services:
        - name: httpnginx
          port: 80
      pathRewritePolicy:
        replacePrefix:
        - prefix: /prefix
          replacement: /
```

The plugin ensures that tls.secretName and services.name are translated to their proper names on the host cluster.