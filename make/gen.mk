##@ Code generation

# Local bin cache
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# Tools + versions
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.16.4

ENVTEST ?= $(LOCALBIN)/setup-envtest
ENVTEST_VERSION ?= release-0.19

# API and output dirs
APIS_DIR ?= ./pkg/api
CRD_DIR  ?= ./config/crd/bases

# Where to generate RBAC from
ORCH_PKG ?= ./cmd/orchestrator
DAEMON_PKG ?= ./cmd/daemon

# Helper: install Go tools to LOCALBIN with version pinning
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
  set -e; \
  package=$(2)@$(3); \
  echo "Installing $$package"; \
  rm -f "$(1)" || true; \
  GOBIN=$(LOCALBIN) go install $$package; \
  mv "$(1)" "$(1)-$(3)"; \
}; \
ln -sf "$(1)-$(3)" "$(1)"
endef

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Ensure controller-gen installed
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Ensure envtest installed
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

##@ Generate Go deep-copy (if you use controller-tools object markers)
.PHONY: deepcopy
deepcopy: controller-gen ## Generate deepcopy funcs for APIs
	$(CONTROLLER_GEN) object paths="$(APIS_DIR)/..."

##@ Generate CRDs from Go types
.PHONY: crd
crd: controller-gen ## Generate CRDs into $(CRD_DIR)
	$(CONTROLLER_GEN) crd paths="$(APIS_DIR)/..." output:crd:artifacts:config=$(CRD_DIR)

##@ Generate RBAC from markers
.PHONY: rbac-orchestrator
rbac-orchestrator: controller-gen ## Generate orchestrator RBAC manifests
	@mkdir -p ./config/rbac/orchestrator
	$(CONTROLLER_GEN) rbac:roleName=orchestrator-role paths="$(ORCH_PKG)" output:rbac:dir=./config/rbac/orchestrator

.PHONY: rbac-daemon
rbac-daemon: controller-gen ## Generate daemon RBAC manifests
	@mkdir -p ./config/rbac/daemon
	$(CONTROLLER_GEN) rbac:roleName=daemon-role paths="$(DAEMON_PKG)" output:rbac:dir=./config/rbac/daemon

.PHONY: rbac-all
rbac-all: rbac-orchestrator rbac-daemon ## Generate all RBAC

##@ Aggregate manifest generation
.PHONY: manifests
manifests: crd rbac-all ## Generate CRDs and all RBAC

##@ Client/lister/informer codegen
.PHONY: generate-code
generate-code: ## Generate API clients, listers and informers
	GOFLAGS="-mod=mod" hack/codegen.sh
