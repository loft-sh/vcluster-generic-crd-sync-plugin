install-cert-manager:
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.2/cert-manager.yaml

remove-cert-manager:
	kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.2/cert-manager.yaml

clean: remove-cert-manager
	kubectl delete ns cert-manager
