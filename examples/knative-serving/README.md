#### Install knative serving on host cluster
```
# installing knative serving CRDs
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.6.0/serving-crds.yaml
# installing knative serving core
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.6.0/serving-core.yaml
# installing kourier networking layer
kubectl apply -f https://github.com/knative/net-kourier/releases/download/knative-v1.6.0/kourier.yaml
# configure serving to use kourier by default
sleep 10
kubectl patch configmap/config-network \
    --namespace knative-serving \
    --type merge \
    --patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
# configure serving to use sslip.io as default DNS
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.6.0/serving-default-domain.yaml

```

#### Deploy vcluster with knative plugin
```
vcluster create vcluster -f https://raw.githubusercontent.com/loft-sh/vcluster-generic-crd-sync-plugin/main/examples/knative-serving/plugin.yaml
```

#### Create a knative service
```
cat << EOF | kubectl apply -f -
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: hello
spec:
  template:
    spec:
      containers:
      - image: gcr.io/google-samples/hello-app:1.0
EOF
```