# Include makefiles from make/ folder
include $(wildcard make/*.mk)

# Default target
.DEFAULT_GOAL := help

#TODO
#
#[ ]: Kind - Cluster creation
#[ ]: Kind - Cluster Deletion
#[ ]: Kind -Add images to cluster registry
#[ ]: Kind - Generate custom node images
#[ ] Versioning - Bump My Version
#[ ] Versioning - Major
#[ ] Versioning - Minor
#[ ] Versioning - Patch
#[ ] Generation - CRD generation
#[ ] Generation - RBAC generation
#[ ] Generation - Code generation
#[ ] Generation - webhook TLS generation
#[ ] Manifest - Installation
#[ ] Manifest - Removal
#[ ] Development - Linting
#[ ] Development - Formatting
#[ ] Development - Testing
#[ ] Development - Vetting
#[ ] Development - Run orchestrator
#[ ] Docker - Build orchestrator, daemon, cni and webhook
#[ ] Docker- Push orchestrator, daemon, cni and webhook



##@ Help

.PHONY: help
help:
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
			printf "  %-15s %s\n", target, desc; \
		} \
	' $(MAKEFILE_LIST)



##@ Code generation

.PHONY: generate-code
generate-code: ## Generate api code for the Tenant and Nodestore CRDs
	export GOFLAGS="-mod=mod"
	hack/codegen.sh

##@ Manifest generation

.PHONY: crd
crd: controller-gen ## Generate CRDS for the defined types: Tenant and NodeStore
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=$(CRD_DIR)

.PHONY: rbac-daemon
rbac-daemon: controller-gen ## Generate RBAC configuration for the daemon component
	$(CONTROLLER_GEN) rbac:roleName=agent-role paths=./internal/$(DAEMON_COMPONENT) output:rbac:dir=./$(RBAC_ORCHESTRATOR_DIR)


.PHONY: rbac-orchestrator
rbac-orchestrator: controller-gen ## Generate RBAC configuration for the orchestrator component
	$(CONTROLLER_GEN) rbac:roleName=orchestrator-role paths=./internal/$(ORCHESTRATOR_COMPONENT) output:rbac:dir=./$(RBAC_DAEMON_DIR)

.PHONY: rbac-all
rbac-all: rbac-daemon rbac-orchestrator ## Generate RBAC for all the components



##@ Manifest installation
.PHONY: install
install: ## Install CRDs, RBAC and webhook configuration onto the cluster - make sure the kubeconfig file is pointing to the correct cluster
	kubectl apply -f config/crd/bases
	kubectl apply -f $(RBAC_ORCHESTRATOR_DIR)
	kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
	kubectl -n kube-system patch deployment metrics-server \
  --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
##	kubectl apply -f $(RBAC_DAEMON_DIR)


##@ Development
.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint against code
	$(GOLANGCI_LINT) run

.PHONY: webhook-ssl
webhook-ssl: ## Generate new webhook certificates
	mkdir -p ${TMPDIR}/k8s-webhook-server/serving-certs
	openssl req -x509 \
			-newkey rsa:2048 \
			-nodes \
			-keyout ${TMPDIR}/k8s-webhook-server/serving-certs/tls.key \
			-out ${TMPDIR}/k8s-webhook-server/serving-certs/tls.crt \
			-days 60

##@ Build

.PHONY: bpf-generate
bpf-generate: ## Generate Go bindings for TC/XDP eBPF programs
	go generate ./pkg/ebpf/loader

.PHONY: build-orchestrator
build-orchestrator: ## Build orchestrator docker image
	docker build \
	--build-arg BINARY=$(ORCHESTRATOR_COMPONENT) \
	-f $(DOCKERFILE) \
	-t $(ORCHESTRATOR_IMG) .

docker-build-orchestrator: ## Build orchestrator docker image
	docker build -t setera.com/orchestrator:latest --build-arg CMD_PATH=./cmd/orchestrator/main.go -f Dockerfile .


.PHONY: build-daemon
build-daemon: bpf-generate # Build daemon docker image (ensures eBPF objects are generated)
	docker build \
	--build-arg BINARY=$(DAEMON_COMPONENT) \
	-f $(DOCKERFILE) \
	-t $(DAEMON_IMG) .

.PHONY: build-stub-daemon
build-stub-daemon: ## Build stub-daemon docker image
	docker build \
	--build-arg BINARY=$(STUB_DAEMON_COMPONENT) \
	-f $(DOCKERFILE) \
	-t $(STUB_DAEMON_IMG) .

.PHONY: build-cni
build-cni: ## Build CNI plugin docker image (for installer DS)
	docker build \
	--build-arg BINARY=$(CNI_COMPONENT) \
	-f $(DOCKERFILE) \
	-t $(CNI_IMG) .


##@ Run
.PHONY: run-orchestrator
run-orchestrator:  ## run orchestrator binary from ouside of the cluster
	go run ./$(CMD_ORCHESTRATOR)/main.go

##@ Cluster operations

.PHONY: kind-cluster-dev
kind-cluster: ## Create kind cluster
	kind create cluster --name=setera-cluster --config=config/cluster/kind_cluster_deployment.yaml

.PHONY: kind-cluster-orch-dev
kind-cluster-dev: ## Create kind cluster for orchestrator development
	kind create cluster --name=setera-cluster-orch-dev --config=config/cluster/kind_cluster_orch_dev.yaml

.PHONY: kind-cluster-delete
kind-cluster-delete: ## Delete kind cluster
	kind delete cluster --name=setera-cluster-orch-dev

.PHONY: kind-cluster-load-images
kind-cluster-load-images: ## Load images into the kind cluster
	kind load docker-image $(REGISTRY_REMOTE)/$(ORCHESTRATOR_COMPONENT):

.PHONY: kind-cluster-load-daemon-image
kind-cluster-load-daemon-image: ## Load daemon image into the kind cluster
	kind load docker-image $(DAEMON_IMG) --name=setera-cluster-orch-dev

.PHONY: kind-cluster-load-stub
kind-cluster-load-stub: ## Load stub-daemon image into the kind cluster
	kind load docker-image $(STUB_DAEMON_IMG) --name=setera-cluster-orch-dev
	kind load docker-image $(CNI_IMG) --name=setera-cluster-orch-dev

.PHONY: kind-cluster-load-orchestrator-image
kind-cluster-load-orchestrator-image: ## Load orchestrator image into the kind cluster
	kind load docker-image $(ORCHESTRATOR_IMG) --name=setera-cluster-orch-dev

.PHONY: create-node-image
create-node-image: ## Create custom kind node image


##@ eBPF Testing

.PHONY: ebpf-cluster
ebpf-cluster: ## Create the eBPF test Kind cluster (separate from main cluster)
	$(MAKE) -C eBPF_test cluster

.PHONY: ebpf-cluster-delete
ebpf-cluster-delete: ## Delete the eBPF test Kind cluster
	$(MAKE) -C eBPF_test cluster-delete

.PHONY: ebpf-deploy
ebpf-deploy: ## Build and deploy eBPF firewall to the test cluster
	$(MAKE) -C eBPF_test deploy

.PHONY: ebpf-clean
ebpf-clean: ## Clean eBPF test environment
	$(MAKE) -C eBPF_test nuke


##@ Installation

.PHONY: daemon
daemon: generate-code crd install build-daemon kind-cluster-load-daemon-image ## Install the daemon component
	kubectl delete -f config/cluster/local_daemon.yaml
	kubectl apply -f config/cluster/local_daemon.yaml

.PHONY: orchestrator
orchestrator: build-orchestrator kind-cluster-load-orchestrator-image ## Deploy orchestrator using local_daemon.yaml (combined manifest)
	kubectl apply -f config/cluster/local_daemon.yaml



## Docker variables

REGISTRY_REMOTE ?= remote.example.com ## Local Docker registry
REGISTRY_LOCAL ?= local.example.com ## Remote Docker registry
DOCKERFILE ?= Dockerfile

## Versioning variables
DAEMON_IMG ?= setera-$(DAEMON_COMPONENT):$(DAEMON_VERSION)
DAEMON_VERSION ?= v0.1.0

ORCHESTRATOR_IMG ?= setera-$(ORCHESTRATOR_COMPONENT):$(ORCHESTRATOR_VERSION)
ORCHESTRATOR_VERSION ?= v0.1.0
DOCKERFILE ?= Dockerfile

STUB_DAEMON_COMPONENT ?= stub-daemon
STUB_DAEMON_VERSION ?= dev
STUB_DAEMON_IMG ?= daemon-uds-stub:$(STUB_DAEMON_VERSION)

CNI_COMPONENT ?= cni
CNI_VERSION ?= dev
CNI_IMG ?= cni-uds-stub:$(CNI_VERSION)

#CMD variables
CMD_ORCHESTRATOR ?= cmd/orchestrator/
CMD_DAEMON ?=cmd/daemon/

#Component variables
ORCHESTRATOR_COMPONENT ?= orchestrator
DAEMON_COMPONENT ?=daemon

##Output directories

RBAC_ORCHESTRATOR_DIR = config/rbac/orchestrator
RBAC_DAEMON_DIR = config/rbac/daemon
CRD_DIR ?= config/crd/bases


##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

##SSL DIR
TMPDIR = ./

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.16.4
ENVTEST_VERSION ?= release-0.19

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))


# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist.
# $1 - target path with name of the binary
# $2 - package url wich can be installed
# $3 - specific version of the package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef
