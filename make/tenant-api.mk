TENANT_API_IMG ?= tenant-api:ebpf-refactor-v2
TENANT_API_DIR ?= tenant-api/tenant-api

.PHONY: build-tenant-api
build-tenant-api: ## Build the refactored tenant API image
	docker build --network=host \
		-f $(TENANT_API_DIR)/deploy/Dockerfile \
		-t $(TENANT_API_IMG) \
		$(TENANT_API_DIR)

.PHONY: load-tenant-api-image
load-tenant-api-image: ## Import the tenant API image into the master node's containerd
	docker save $(TENANT_API_IMG) | ctr -n k8s.io images import -

.PHONY: deploy-tenant-api
deploy-tenant-api: ## Deploy the tenant API (create tenant-api-auth first)
	kubectl get secret tenant-api-auth -n default >/dev/null
	kubectl apply -f $(TENANT_API_DIR)/deploy/default-tenant.yaml
	kubectl apply -f $(TENANT_API_DIR)/deploy/rbac.yaml
	kubectl apply -f $(TENANT_API_DIR)/deploy/service.yaml
	kubectl apply -f $(TENANT_API_DIR)/deploy/deployment.yaml

.PHONY: install-tenant-api
install-tenant-api: build-tenant-api load-tenant-api-image deploy-tenant-api ## Build, load and deploy the tenant API
