install-knative-serving:
	# installing knative serving CRDs
	kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.5.0/serving-crds.yaml
	# installing knative serving core
	kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.5.0/serving-core.yaml
	# installing kourier networking layer
	kubectl apply -f https://github.com/knative/net-kourier/releases/download/knative-v1.5.0/kourier.yaml
	# configure serving to use kourier by default
	kubectl patch configmap/config-network \
		--namespace knative-serving \
		--type merge \
		--patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
	# configure serving to use sslip.io as default DNS
	kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.5.0/serving-default-domain.yaml

remove-knative-serving:
	# remove default serving DNS
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-v1.5.0/serving-default-domain.yaml
	# removing kourier networking layer
	kubectl delete -f https://github.com/knative/net-kourier/releases/download/knative-v1.5.0/kourier.yaml
	# removing knative serving core
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-v1.5.0/serving-core.yaml
	# removing knative serving CRDs
	kubectl delete -f https://github.com/knative/serving/releases/download/knative-v1.5.0/serving-crds.yaml

