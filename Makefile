# Include shared make fragments.
include $(wildcard make/*.mk)

.DEFAULT_GOAL := help

## -----------------------------------------------------------------------------
## Local Setera / kind configuration
## -----------------------------------------------------------------------------

KIND_CLUSTER_NAME ?= setera-cluster
KIND_CONTEXT      ?= kind-$(KIND_CLUSTER_NAME)
KIND_CONFIG       ?= config/cluster/kind_cluster_deployment.yaml

KIND_NODE_IMG        ?= kind-node-nettool:v1.33.1
KIND_NODE_DOCKERFILE ?= Dockerfile.kindnode

# Local runtime image tags. These must match config/cluster/local_daemon.yaml.
DAEMON_COMPONENT ?= daemon
DAEMON_VERSION   ?= v0.1.0
DAEMON_IMG       ?= setera-$(DAEMON_COMPONENT):$(DAEMON_VERSION)

ORCHESTRATOR_COMPONENT ?= orchestrator
ORCHESTRATOR_VERSION   ?= v0.1.0
ORCHESTRATOR_IMG       ?= setera-$(ORCHESTRATOR_COMPONENT):$(ORCHESTRATOR_VERSION)

CNI_COMPONENT ?= cni
CNI_VERSION   ?= dev
CNI_IMG       ?= cni-uds-stub:$(CNI_VERSION)

# Legacy stub image is kept for the old stub-only development flow.
STUB_DAEMON_COMPONENT ?= stub-daemon
STUB_DAEMON_VERSION   ?= dev
STUB_DAEMON_IMG       ?= daemon-uds-stub:$(STUB_DAEMON_VERSION)

# Local kind binaries and temporary runtime image contexts.
KIND_BUILD_DIR ?= $(LOCALBIN)/kind
KIND_GOOS      ?= linux
KIND_GOARCH    ?= amd64

RBAC_ORCHESTRATOR_DIR ?= config/rbac/orchestrator
RBAC_DAEMON_DIR       ?= config/rbac/daemon
CRD_DIR               ?= config/crd/bases

LOCAL_DEPLOYMENT      ?= config/cluster/local_daemon.yaml
WEBHOOK_CONFIGURATION ?= setera-pod-admission


##@ Help

.PHONY: help
help: ## Show available make targets
	@awk ' \
		/^##@/ { \
			section = substr($$0, 5); \
			printf "\n%s\n", section; \
		} \
		/^[a-zA-Z0-9_-]+:.*? ##/ { \
			split($$0,a,":"); \
			target = a[1]; \
			match($$0, /## (.*)/, m); \
			desc = m[1]; \
			printf "  %-30s %s\n", target, desc; \
		} \
	' $(MAKEFILE_LIST)


##@ eBPF

.PHONY: bpf-generate
bpf-generate: ## Generate Go bindings for TC/XDP eBPF programs
	$(GO) generate ./pkg/ebpf/loader


##@ Standard Docker builds

DOCKERFILE ?= Dockerfile

.PHONY: build-orchestrator
build-orchestrator: ## Build orchestrator with the repository Dockerfile
	$(DOCKER) build \
		--build-arg BINARY=$(ORCHESTRATOR_COMPONENT) \
		-f $(DOCKERFILE) \
		-t $(ORCHESTRATOR_IMG) .

.PHONY: build-daemon
build-daemon: bpf-generate ## Build daemon with the repository Dockerfile
	$(DOCKER) build \
		--build-arg BINARY=$(DAEMON_COMPONENT) \
		-f $(DOCKERFILE) \
		-t $(DAEMON_IMG) .

.PHONY: build-cni
build-cni: ## Build CNI installer image with the repository Dockerfile
	$(DOCKER) build \
		--build-arg BINARY=$(CNI_COMPONENT) \
		-f $(DOCKERFILE) \
		-t $(CNI_IMG) .

.PHONY: build-stub-daemon
build-stub-daemon: ## Build legacy stub daemon image
	$(DOCKER) build \
		--build-arg BINARY=$(STUB_DAEMON_COMPONENT) \
		-f $(DOCKERFILE) \
		-t $(STUB_DAEMON_IMG) .


##@ Local kind images

.PHONY: kind-build-daemon-binary
kind-build-daemon-binary: bpf-generate ## Build the Linux daemon binary for kind
	@mkdir -p $(KIND_BUILD_DIR)/daemon
	CGO_ENABLED=0 GOOS=$(KIND_GOOS) GOARCH=$(KIND_GOARCH) \
		$(GO) build -o $(KIND_BUILD_DIR)/daemon/daemon ./cmd/daemon

.PHONY: kind-build-orchestrator-binary
kind-build-orchestrator-binary: ## Build the Linux orchestrator binary for kind
	@mkdir -p $(KIND_BUILD_DIR)/orchestrator
	CGO_ENABLED=0 GOOS=$(KIND_GOOS) GOARCH=$(KIND_GOARCH) \
		$(GO) build -o $(KIND_BUILD_DIR)/orchestrator/orchestrator ./cmd/orchestrator

.PHONY: kind-build-cni-binary
kind-build-cni-binary: ## Build the Linux CNI binary for kind
	@mkdir -p $(KIND_BUILD_DIR)/cni
	CGO_ENABLED=0 GOOS=$(KIND_GOOS) GOARCH=$(KIND_GOARCH) \
		$(GO) build -o $(KIND_BUILD_DIR)/cni/cni ./cmd/cni

.PHONY: kind-build-daemon-image
kind-build-daemon-image: kind-build-daemon-binary ## Build the local daemon runtime image
	@printf '%s\n' \
		'FROM alpine:latest' \
		'RUN apk add --no-cache ca-certificates iptables' \
		'COPY daemon /daemon' \
		'ENTRYPOINT ["/daemon"]' \
		> $(KIND_BUILD_DIR)/daemon/Dockerfile
	$(DOCKER) build \
		-f $(KIND_BUILD_DIR)/daemon/Dockerfile \
		-t $(DAEMON_IMG) \
		$(KIND_BUILD_DIR)/daemon

.PHONY: kind-build-orchestrator-image
kind-build-orchestrator-image: kind-build-orchestrator-binary ## Build the local orchestrator runtime image
	@printf '%s\n' \
		'FROM alpine:latest' \
		'RUN apk add --no-cache ca-certificates' \
		'COPY orchestrator /orchestrator' \
		'ENTRYPOINT ["/orchestrator"]' \
		> $(KIND_BUILD_DIR)/orchestrator/Dockerfile
	$(DOCKER) build \
		-f $(KIND_BUILD_DIR)/orchestrator/Dockerfile \
		-t $(ORCHESTRATOR_IMG) \
		$(KIND_BUILD_DIR)/orchestrator

.PHONY: kind-build-cni-image
kind-build-cni-image: kind-build-cni-binary ## Build the local CNI installer image
	@printf '%s\n' \
		'FROM alpine:latest' \
		'COPY cni /cni' \
		> $(KIND_BUILD_DIR)/cni/Dockerfile
	$(DOCKER) build \
		-f $(KIND_BUILD_DIR)/cni/Dockerfile \
		-t $(CNI_IMG) \
		$(KIND_BUILD_DIR)/cni

.PHONY: kind-build-images
kind-build-images: kind-build-daemon-image kind-build-orchestrator-image kind-build-cni-image ## Build all images required by local Setera E2E


##@ kind cluster

.PHONY: kind-node-image
kind-node-image: ## Build the custom kind node image with networking/eBPF tools
	$(DOCKER) build \
		-f $(KIND_NODE_DOCKERFILE) \
		-t $(KIND_NODE_IMG) .

.PHONY: kind-cluster
kind-cluster: kind-node-image ## Create the main Setera kind cluster if it does not already exist
	@if kind get clusters | grep -qx '$(KIND_CLUSTER_NAME)'; then \
		echo "kind cluster $(KIND_CLUSTER_NAME) already exists"; \
	else \
		kind create cluster \
			--name $(KIND_CLUSTER_NAME) \
			--config $(KIND_CONFIG); \
	fi

.PHONY: kind-cluster-check
kind-cluster-check: ## Fail if the main Setera kind cluster does not exist
	@kind get clusters | grep -qx '$(KIND_CLUSTER_NAME)' || { \
		echo "kind cluster $(KIND_CLUSTER_NAME) does not exist"; \
		echo "run: make kind-cluster"; \
		exit 1; \
	}

.PHONY: kind-cluster-delete
kind-cluster-delete: ## Delete the main Setera kind cluster
	kind delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: kind-cluster-reset
kind-cluster-reset: ## Recreate the main Setera kind cluster
	-$(MAKE) kind-cluster-delete
	$(MAKE) kind-cluster


##@ Load images into kind

.PHONY: kind-load-daemon-image
kind-load-daemon-image: kind-cluster-check kind-build-daemon-image ## Build and load daemon image into every kind node
	kind load docker-image $(DAEMON_IMG) --name $(KIND_CLUSTER_NAME)

.PHONY: kind-load-orchestrator-image
kind-load-orchestrator-image: kind-cluster-check kind-build-orchestrator-image ## Build and load orchestrator image into every kind node
	kind load docker-image $(ORCHESTRATOR_IMG) --name $(KIND_CLUSTER_NAME)

.PHONY: kind-load-cni-image
kind-load-cni-image: kind-cluster-check kind-build-cni-image ## Build and load CNI installer image into every kind node
	kind load docker-image $(CNI_IMG) --name $(KIND_CLUSTER_NAME)

.PHONY: kind-load-images
kind-load-images: kind-load-daemon-image kind-load-orchestrator-image kind-load-cni-image ## Build and load all local E2E images into every kind node


##@ Local kind deployment

.PHONY: kind-install-manifests
kind-install-manifests: manifests kind-cluster-check ## Install Setera CRDs and generated RBAC into the kind cluster
	kubectl --context $(KIND_CONTEXT) apply -f $(CRD_DIR)
	kubectl --context $(KIND_CONTEXT) apply -f $(RBAC_DAEMON_DIR)
	kubectl --context $(KIND_CONTEXT) apply -f $(RBAC_ORCHESTRATOR_DIR)

.PHONY: kind-apply
kind-apply: kind-install-manifests ## Apply daemon + orchestrator local E2E manifest
	kubectl --context $(KIND_CONTEXT) apply -f $(LOCAL_DEPLOYMENT)

.PHONY: kind-rollout
kind-rollout: kind-cluster-check ## Restart daemon/orchestrator so newly loaded local images are used
	kubectl --context $(KIND_CONTEXT) rollout restart daemonset/daemon
	kubectl --context $(KIND_CONTEXT) rollout restart deployment/orchestrator
	kubectl --context $(KIND_CONTEXT) rollout status daemonset/daemon --timeout=180s
	kubectl --context $(KIND_CONTEXT) rollout status deployment/orchestrator --timeout=180s

.PHONY: kind-deploy
kind-deploy: ## Build/load images and deploy daemon + orchestrator to the existing kind cluster
	$(MAKE) kind-load-images
	$(MAKE) kind-apply
	$(MAKE) kind-rollout

.PHONY: kind-e2e
kind-e2e: ## Create cluster if needed, build/load images, and deploy complete local Setera E2E
	$(MAKE) kind-cluster
	$(MAKE) kind-deploy

.PHONY: kind-redeploy
kind-redeploy: ## Rebuild local images, reload them into kind, and restart Setera
	$(MAKE) kind-deploy

.PHONY: kind-undeploy
kind-undeploy: kind-cluster-check ## Remove daemon + orchestrator local E2E runtime objects
	-kubectl --context $(KIND_CONTEXT) delete -f $(LOCAL_DEPLOYMENT)
	-kubectl --context $(KIND_CONTEXT) delete mutatingwebhookconfiguration $(WEBHOOK_CONFIGURATION)

.PHONY: kind-status
kind-status: kind-cluster-check ## Show local Setera E2E status
	kubectl --context $(KIND_CONTEXT) get nodes
	kubectl --context $(KIND_CONTEXT) get daemonset daemon
	kubectl --context $(KIND_CONTEXT) get deployment orchestrator
	kubectl --context $(KIND_CONTEXT) get service setera-webhook
	kubectl --context $(KIND_CONTEXT) get mutatingwebhookconfiguration $(WEBHOOK_CONFIGURATION)
	kubectl --context $(KIND_CONTEXT) get pods -o wide

.PHONY: kind-logs-daemon
kind-logs-daemon: kind-cluster-check ## Show daemon logs from all kind nodes
	kubectl --context $(KIND_CONTEXT) logs -l app=daemon --prefix --tail=200

.PHONY: kind-logs-orchestrator
kind-logs-orchestrator: kind-cluster-check ## Show orchestrator logs
	kubectl --context $(KIND_CONTEXT) logs deployment/orchestrator --tail=200


##@ Local development

.PHONY: run-orchestrator
run-orchestrator: ## Run orchestrator outside the cluster
	$(GO) run ./cmd/orchestrator


##@ eBPF testing

.PHONY: ebpf-cluster
ebpf-cluster: ## Create the separate eBPF test kind cluster
	$(MAKE) -C eBPF_test cluster

.PHONY: ebpf-cluster-delete
ebpf-cluster-delete: ## Delete the separate eBPF test kind cluster
	$(MAKE) -C eBPF_test cluster-delete

.PHONY: ebpf-deploy
ebpf-deploy: ## Build and deploy the eBPF firewall to the test cluster
	$(MAKE) -C eBPF_test deploy

.PHONY: ebpf-clean
ebpf-clean: ## Clean the eBPF test environment
	$(MAKE) -C eBPF_test nuke
