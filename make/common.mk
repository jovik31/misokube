
SHELL := /bin/bash

# Core toolchain
GO      ?= go
DOCKER  ?= docker
BUILDX  ?= docker buildx

# Build metadata
VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
LDFLAGS ?= -s -w \
  -X 'main.version=$(VERSION)' \
  -X 'main.commit=$(COMMIT)' \
  -X 'main.date=$(DATE)'

# Images (example)
REGISTRY ?= ghcr.io/your-org
BINARY   ?= orchestrator
IMAGE    ?= $(REGISTRY)/setera-$(BINARY)

# ...other repo-wide vars...