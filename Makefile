IMG ?= cf-tunnel-operator:latest

.PHONY: all
all: build

.PHONY: build
build:
	go build -o bin/manager main.go

.PHONY: docker-build
docker-build:
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push:
	docker push ${IMG}

# Minikube workflow: build image directly inside minikube's Docker daemon
.PHONY: minikube-build
minikube-build:
	@eval $$(minikube docker-env) && docker build -t ${IMG} .

.PHONY: create-configs
create-configs:
	kubectl apply -f config/crd/
	kubectl apply -f config/rbac/
	kubectl apply -f config/manager/

.PHONY: create-namespace
create-namespace:
	kubectl create namespace cf-tunnel-system --dry-run=client -o yaml | kubectl apply -f -

# Load CF secrets from .env file into minikube as a Kubernetes Secret
.PHONY: load-secrets
load-secrets:
	@if [ ! -f .env ]; then \
		echo "Error: .env file not found. Create one with CF_API_TOKEN, CF_ACCOUNT_ID, CF_ZONE_ID"; \
		exit 1; \
	fi
	@echo "Loading secrets from .env into minikube..."
	@echo "Note: .env values should NOT be quoted (e.g. CF_API_TOKEN=abc, not CF_API_TOKEN=\"abc\")"
	@kubectl create secret generic cf-operator-secrets \
		--namespace=cf-tunnel-system \
		--from-env-file=.env \
		--dry-run=client -o yaml | kubectl apply -f -
	@echo "Secrets loaded. Verifying..."
	@kubectl get secret cf-operator-secrets -n cf-tunnel-system -o jsonpath='{.data}' | \
		python3 -c "import sys,json,base64; d=json.load(sys.stdin); \
			print('  CF_ACCOUNT_ID:', base64.b64decode(d.get('CF_ACCOUNT_ID','')).decode() if 'CF_ACCOUNT_ID' in d else 'MISSING'); \
			print('  CF_ZONE_ID:', base64.b64decode(d.get('CF_ZONE_ID','')).decode() if 'CF_ZONE_ID' in d else 'MISSING'); \
			print('  CF_API_TOKEN:', 'SET (' + str(len(base64.b64decode(d.get('CF_API_TOKEN','')).decode())) + ' chars)' if 'CF_API_TOKEN' in d else 'MISSING')"

# Full minikube workflow: build image, load secrets, create configs, and deploy
.PHONY: deploy
deploy: minikube-build create-namespace load-secrets create-configs
	@echo "Minikube deployment complete."

.PHONY: undeploy
undeploy:
	kubectl delete -f config/samples/ --ignore-not-found=true
	kubectl delete cloudflaretunnels --all --all-namespaces --ignore-not-found=true
	@echo "Waiting for finalizers..."
	@sleep 5
	kubectl delete -f config/crd/ --ignore-not-found=true
	kubectl delete -f config/manager/ --ignore-not-found=true
	kubectl delete -f config/rbac/ --ignore-not-found=true
	kubectl delete namespace cf-tunnel-system --ignore-not-found=true

.PHONY: sample-ingress
sample-ingress:
	kubectl apply -f config/samples/ingress_sample.yaml

.PHONY: delete-sample-ingress
delete-sample-ingress:
	kubectl delete -f config/samples/ingress_sample.yaml

.PHONY: sample-app
sample-app:
	kubectl apply -f config/samples/app_sample.yaml

.PHONY: delete-sample-app
delete-sample-app:
	kubectl delete -f config/samples/app_sample.yaml

.PHONY: run
run:
	go run ./main.go

.PHONY: tidy
tidy:
	go mod tidy
