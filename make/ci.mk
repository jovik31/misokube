# Tool versions
TRIVY_VERSION      ?= 0.54.1
GOLANGCI_VERSION   ?= v1.61.0
GOSEC_VERSION      ?= latest
HADOLINT_VERSION   ?= latest
KUBELINTER_VERSION ?= v0.6.7

# Tool images
TRIVY_IMG      ?= aquasec/trivy:$(TRIVY_VERSION)
GOLANGCI_IMG   ?= golangci/golangci-lint:$(GOLANGCI_VERSION)
GOSEC_IMG      ?= securego/gosec:$(GOSEC_VERSION)
HADOLINT_IMG   ?= hadolint/hadolint:$(HADOLINT_VERSION)
KUBELINTER_IMG ?= docker.io/stackrox/kube-linter:$(KUBELINTER_VERSION)

# Caches (mounted into tool containers)
TRIVY_CACHE_DIR ?= $(HOME)/.cache/trivy
GOMODCACHE      ?= $(HOME)/go/pkg/mod
GOCACHE         ?= $(HOME)/.cache/go-build

.PHONY: tool-caches tools-pull
tool-caches:
	mkdir -p "$(TRIVY_CACHE_DIR)" "$(GOMODCACHE)" "$(GOCACHE)"

tools-pull:
	docker pull $(TRIVY_IMG)
	docker pull $(GOLANGCI_IMG)
	docker pull $(GOSEC_IMG)
	docker pull $(HADOLINT_IMG)
	docker pull $(KUBELINTER_IMG)

# Standard CI (native) targets
.PHONY: fmt vet test race cover lint vuln sast sca ci
fmt:  ; $(GO) fmt ./...
vet:  ; $(GO) vet ./...
test: ; $(GO) test -count=1 ./...
race: ; $(GO) test -race -count=1 ./...
cover:; $(GO) test -coverprofile=coverage.out ./...
lint: ; golangci-lint run ./...
vuln: ; govulncheck ./...
sast: ; gosec ./...
sca:  ; trivy fs --scanners vuln,secret,config --exit-code 1 --ignore-unfixed .
ci: fmt vet lint test vuln sast sca

# Dockerized alternatives (no local installs)
.PHONY: lint-docker sast-docker sca-fs-docker hadolint-docker kube-linter-docker
lint-docker: tool-caches
	docker run --rm -v "$$PWD":/work -w /work \
	  -v "$(GOMODCACHE)":/go/pkg/mod -v "$(GOCACHE)":/root/.cache/go-build \
	  $(GOLANGCI_IMG) golangci-lint run ./...

sast-docker:
	docker run --rm -v "$$PWD":/work -w /work $(GOSEC_IMG) ./...

sca-fs-docker: tool-caches
	docker run --rm -v "$$PWD":/project -w /project \
	  -v "$(TRIVY_CACHE_DIR)":/root/.cache/ \
	  $(TRIVY_IMG) fs --scanners vuln,secret,config --exit-code 1 --ignore-unfixed .

hadolint-docker:
	docker run --rm -i $(HADOLINT_IMG) < Dockerfile

kube-linter-docker:
	docker run --rm -v "$$PWD":/repo $(KUBELINTER_IMG) lint /repo/config


# Standard unit tests
.PHONY: unit
unit:
	$(GO) test -v ./internal/orchestrator -count=1

# Ginkgo unit tests
unit-bdd:
	$(GO) test -v ./internal/orchestrator -ginkgo.v -count=1  