IMG ?= ghcr.io/candeiasalexandre/k8s-cf-tunnel-operator:latest

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
	@eval $$(minikube docker-env) && docker build -t cf-tunnel-operator:latest .

# Helm-based minikube workflow — sources .env for Cloudflare credentials
.PHONY: helm-deploy-minikube
helm-deploy-minikube: minikube-build
	@if [ ! -f .env ]; then \
		echo "Error: .env file not found. Create one with CF_API_TOKEN, CF_ACCOUNT_ID, CF_ZONE_ID"; \
		exit 1; \
	fi
	@. ./.env && \
	helm upgrade --install cf-tunnel-operator charts/cf-tunnel-operator/ \
		--namespace cf-tunnel-system --create-namespace \
		--values charts/cf-tunnel-operator/values-minikube.yaml \
		--set cloudflare.apiToken="$${CF_API_TOKEN}" \
		--set cloudflare.accountId="$${CF_ACCOUNT_ID}" \
		--set cloudflare.zoneId="$${CF_ZONE_ID}"
	@echo "Helm deployment complete. Run 'kubectl get pods -n cf-tunnel-system' to verify."

.PHONY: helm-undeploy-minikube
helm-undeploy-minikube:
	@echo "Deleting sample Ingresses to stop Ingress controller triggers..."
	kubectl delete -f config/samples/ingress_sample.yaml --ignore-not-found=true 2>/dev/null || true
	@echo "Deleting all CloudflareTunnel CRs (operator will clean up Cloudflare resources)..."
	kubectl delete cloudflaretunnels.cloudflare.example.com --all -A --ignore-not-found=true 2>/dev/null || true
	@echo "Waiting for operator to finish cleanup (30s)..."
	sleep 30
	@echo "Removing any stuck finalizers..."
	kubectl patch cloudflaretunnels.cloudflare.example.com -A -p '{"metadata":{"finalizers":null}}' --type=merge 2>/dev/null || true
	@echo "Uninstalling operator..."
	helm uninstall cf-tunnel-operator -n cf-tunnel-system 2>/dev/null || true
	@echo "Deleting CRD..."
	kubectl delete crd cloudflaretunnels.cloudflare.example.com --ignore-not-found=true 2>/dev/null || true
	@echo "Cleanup complete."

.PHONY: helm-dry-run-minikube
helm-dry-run-minikube:
	@if [ ! -f .env ]; then \
		echo "Error: .env file not found"; \
		exit 1; \
	fi
	@. ./.env && \
	helm template cf-tunnel-operator charts/cf-tunnel-operator/ \
		--values charts/cf-tunnel-operator/values-minikube.yaml \
		--set cloudflare.apiToken="$${CF_API_TOKEN}" \
		--set cloudflare.accountId="$${CF_ACCOUNT_ID}" \
		--set cloudflare.zoneId="$${CF_ZONE_ID}"

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

.PHONY: helm-lint
helm-lint:
	helm lint charts/cf-tunnel-operator/

.PHONY: helm-package
helm-package:
	helm package charts/cf-tunnel-operator/ -d charts/

.PHONY: helm-install
helm-install:
	helm upgrade --install cf-tunnel-operator charts/cf-tunnel-operator/ \
		--namespace cf-tunnel-system --create-namespace

.PHONY: helm-uninstall
helm-uninstall:
	helm uninstall cf-tunnel-operator -n cf-tunnel-system

.PHONY: helm-template
helm-template:
	helm template cf-tunnel-operator charts/cf-tunnel-operator/

.PHONY: tidy
tidy:
	go mod tidy
