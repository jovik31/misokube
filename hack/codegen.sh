#!/usr/bin/env bash

set -euo pipefail

log() {
	printf '[codegen] %s\n' "$*" >&2
}

fail() {
	printf '[codegen] ERROR: %s\n' "$*" >&2
	exit 1
}

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

THIS_PKG="${THIS_PKG:-github/setera}"
API_DIR="${API_DIR:-${REPO_ROOT}/pkg/api}"
OUTPUT_DIR="${OUTPUT_DIR:-${REPO_ROOT}/pkg/generated}"
OUTPUT_PKG="${OUTPUT_PKG:-${THIS_PKG}/pkg/generated}"
BOILERPLATE="${BOILERPLATE:-${REPO_ROOT}/hack/boilerplate.go.txt}"
VERIFY="${VERIFY:-false}"

cd "${REPO_ROOT}"

command -v go >/dev/null 2>&1 || fail "go binary not found in PATH"

if ! go env GOCACHE >/dev/null 2>&1 || [ ! -w "$(go env GOCACHE)" ]; then
	export GOCACHE="${GOCACHE:-/tmp/setera-go-build-cache}"
	mkdir -p "${GOCACHE}"
	log "using writable Go build cache: ${GOCACHE}"
fi

CODEGEN_VERSION="${CODEGEN_VERSION:-$(go list -m -f '{{.Version}}' k8s.io/code-generator 2>/dev/null || true)}"
[ -n "${CODEGEN_VERSION}" ] || fail "k8s.io/code-generator is not required in go.mod"

GOMODCACHE="$(go env GOMODCACHE)"
CODEGEN_PKG="${CODEGEN_PKG:-${GOMODCACHE}/k8s.io/code-generator@${CODEGEN_VERSION}}"

if [ ! -f "${CODEGEN_PKG}/kube_codegen.sh" ]; then
	log "code-generator ${CODEGEN_VERSION} not found in module cache; downloading"
	go mod download k8s.io/code-generator
fi

[ -f "${CODEGEN_PKG}/kube_codegen.sh" ] || fail "kube_codegen.sh not found at ${CODEGEN_PKG}"
[ -d "${API_DIR}" ] || fail "API directory not found: ${API_DIR}"
[ -f "${BOILERPLATE}" ] || fail "boilerplate file not found: ${BOILERPLATE}"

log "repo root: ${REPO_ROOT}"
log "code-generator: ${CODEGEN_PKG}"
log "api dir: ${API_DIR}"
log "output dir: ${OUTPUT_DIR}"
log "output pkg: ${OUTPUT_PKG}"

# shellcheck source=/dev/null
source "${CODEGEN_PKG}/kube_codegen.sh"

kube::codegen::gen_helpers \
	"${API_DIR}" \
	--boilerplate "${BOILERPLATE}"

kube::codegen::gen_client \
	--with-applyconfig \
	--with-watch \
	--output-dir "${OUTPUT_DIR}" \
	--output-pkg "${OUTPUT_PKG}" \
	--boilerplate "${BOILERPLATE}" \
	"${API_DIR}"

if [ "${VERIFY}" = "true" ]; then
	log "verifying generated packages"
	go test ./pkg/generated/...
fi

log "code generation complete"
