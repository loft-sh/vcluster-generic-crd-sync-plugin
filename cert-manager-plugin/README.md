# vcluster-cert-manager-plugin

This plugin syncs cert-manager issuers and certificates across the host and virtual cluster. It allows to reuse a single cert-manager of the host cluster for all vclusters that run this plugin.

## Quickstart

Deploy vcluster with the plugin:
```
vcluster create my-vcluster -n my-vcluster -f https://raw.githubusercontent.com/FabianKramm/vcluster-cert-manager-plugin/main/plugin.yaml
```

Now wait until vcluster has started and deploy a cert-manager issuer into the vcluster:

`issuer.yaml`:
```yaml
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: letsencrypt-staging
  namespace: default
spec:
  acme:
    # The ACME server URL
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    # Email address used for ACME registration
    email: user@example.com
    # Name of a secret used to store the ACME account private key
    privateKeySecretRef:
      name: letsencrypt-staging
    # Enable the HTTP-01 challenge provider
    solvers:
    # An empty 'selector' means that this solver matches all domains
    - selector: {}
      http01:
        ingress:
          class: nginx
```

```
vcluster connect my-vcluster -n my-vcluster -- kubectl apply -f issuer.yaml
```

Now create a certificate or ingress using this issuer:
`certificate.yaml`:
```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com
  namespace: default
spec:
  secretName: example-com-tls
  issuerRef:
    name: letsencrypt-staging
  commonName: example.com
  dnsNames:
  - www.example.com
```

```
vcluster connect my-vcluster -n my-vcluster -- kubectl apply -f certificate.yaml
```

