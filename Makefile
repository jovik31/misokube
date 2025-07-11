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
	hack/update-codegen.sh

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
	kubectl apply -f $(RBAC_DAEMON_DIR)


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
webhook-ssl: ## Generate new weobhook certificates
	mkdir -p ${TMPDIR}/k8s-webhook-server/serving-certs
	openssl req -x509 \
			-newkey rsa:2048 \
			-nodes \
			-keyout ${TMPDIR}/k8s-webhook-server/serving-certs/tls.key \
			-out ${TMPDIR}/k8s-webhook-server/serving-certs/tls.crt \
			-days 60

##@ Build

.PHONY: build-orchestrator
build-orchestrator: ## Build orchestrator docker image
	docker build \ 
	--build-arg CMD_PATH=$(CMD_ORCHESTRATOR) \
	-f $(DOCKERFILE) \
	-t $(REGISTRY)/$(ORCHESTRATOR_COMPONENT)r:$(IMAGE_TAG) .

docker-build-orchestrator: ## Build orchestrator docker image
	docker build -t setera.com/orchestrator:latest --build-arg CMD_PATH=./cmd/orchestrator/main.go -f Dockerfile .

.PHONY: build-daemon
build-daemon: # Build daemon docker image
	docker build \
	--build-arg CMD_PATH=$(CMD_DAEMON)
	-f $(DOCKERFILE)
	-t $(REGISTRY)/$(DAEMON_COMPONENT):$(IMAGE_TAG)


##@ Run
.PHONY: run-orchestrator
run-orchestrator:  ## run orchestrator binary from ouside of the cluster
	go run ./$(CMD_ORCHESTRATOR)/main.go

##@ Cluster operations

.PHONY: kind-cluster
kind-cluster: ## Create kind cluster
	kind create cluster --name=setera-cluster --config=config/cluster/kind_cluster_deployment.yaml ## create a kind cluster for testing

.PHONY: create-node-image
create-node-image: ## Create custom kind node image

## Docker variables

REGISTRY_REMOTE ?= remote.example.com ## Local Docker registry
REGISTRY_LOCAL ?= local.example.com ## Remote Docker registry
DOCKERFILE ?= Dockerfile

## Versioning variables
DAEMON_IMG ?= $(DAEMON_COMPONENT):$(DAEMON_VERSION)
DAEMON_VERSION ?= v0.1.0

ORCHESTRATOR_IMG ?= $(ORCHESTRATOR_COMPONENT):$(ORCHESTRATOR_VERSION)
ORCHESTRATOR_VERSION ?= v0.1.0
DOCKERFILE ?= Dockerfile

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