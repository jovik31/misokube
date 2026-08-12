#!/usr/bin/env bash

set -Eeuo pipefail

KIND_CONTEXT="${KIND_CONTEXT:-kind-setera-cluster}"
EXTERNAL_IP="${SETERA_E2E_EXTERNAL_IP:-1.1.1.1}"
HTTP_PORT="${SETERA_E2E_HTTP_PORT:-18080}"

TENANT_A="tenant-a"
TENANT_B="tenant-b"

TENANT_LABEL="setera.com/tenant"

SERVICE_A="setera-e2e-tenant-a"
SERVICE_B="setera-e2e-tenant-b"

log() {
    printf '\n==> %s\n' "$*"
}

pass() {
    printf 'PASS: %s\n' "$*"
}

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

k() {
    kubectl --context "$KIND_CONTEXT" "$@"
}

cleanup() {
    k delete service \
        "$SERVICE_A" \
        "$SERVICE_B" \
        --ignore-not-found \
        >/dev/null 2>&1 || true
}

diagnostics() {
    printf '\n=== E2E diagnostics ===\n' >&2

    k get tenants -o wide >&2 || true

    k get nodes \
        -L setera.com/tenant.tenant-a \
        -L setera.com/tenant.tenant-b \
        -L setera.com/vtep-ready \
        >&2 || true

    k get pods -o wide >&2 || true

    k get services \
        "$SERVICE_A" \
        "$SERVICE_B" \
        >&2 || true
}

on_exit() {
    status=$?

    trap - EXIT
    set +e

    if [[ $status -ne 0 ]]; then
        diagnostics
    fi

    cleanup

    exit "$status"
}

trap on_exit EXIT

pod_node() {
    k get pod "$1" \
        -o jsonpath='{.spec.nodeName}'
}

pod_ip() {
    k get pod "$1" \
        -o jsonpath='{.status.podIP}'
}

pods_for_tenant() {
    k get pods \
        -l "${TENANT_LABEL}=$1" \
        -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' |
        sort
}

unique_node_count() {
    local pod

    for pod in "$@"; do
        pod_node "$pod"
        printf '\n'
    done |
        sort -u |
        wc -l
}

find_same_node_pair() {
    local pods=("$@")
    local i
    local j
    local left_node
    local right_node

    for ((i = 0; i < ${#pods[@]}; i++)); do
        left_node="$(pod_node "${pods[$i]}")"

        for ((j = i + 1; j < ${#pods[@]}; j++)); do
            right_node="$(pod_node "${pods[$j]}")"

            if [[ "$left_node" == "$right_node" ]]; then
                printf '%s %s\n' \
                    "${pods[$i]}" \
                    "${pods[$j]}"

                return 0
            fi
        done
    done

    return 1
}

find_cross_node_pair() {
    local pods=("$@")
    local i
    local j
    local left_node
    local right_node

    for ((i = 0; i < ${#pods[@]}; i++)); do
        left_node="$(pod_node "${pods[$i]}")"

        for ((j = i + 1; j < ${#pods[@]}; j++)); do
            right_node="$(pod_node "${pods[$j]}")"

            if [[ "$left_node" != "$right_node" ]]; then
                printf '%s %s\n' \
                    "${pods[$i]}" \
                    "${pods[$j]}"

                return 0
            fi
        done
    done

    return 1
}

expect_ping_allow() {
    local label="$1"
    local source_pod="$2"
    local destination_pod="$3"
    local destination_ip
    local output

    destination_ip="$(pod_ip "$destination_pod")"

    if output="$(
        k exec "$source_pod" -- \
            ping -c 2 -W 1 "$destination_ip" \
            2>&1
    )"; then
        pass "$label"
        return 0
    fi

    printf '%s\n' "$output" >&2

    fail "$label"
}

expect_ping_deny() {
    local label="$1"
    local source_pod="$2"
    local destination_pod="$3"
    local destination_ip
    local output

    destination_ip="$(pod_ip "$destination_pod")"

    if output="$(
        k exec "$source_pod" -- \
            ping -c 2 -W 1 "$destination_ip" \
            2>&1
    )"; then
        printf '%s\n' "$output" >&2

        fail "$label: traffic was allowed"
    fi

    pass "$label"
}

expect_exec_allow() {
    local label="$1"
    local pod="$2"
    local output

    shift 2

    if output="$(
        k exec "$pod" -- "$@" \
            2>&1
    )"; then
        pass "$label"
        return 0
    fi

    printf '%s\n' "$output" >&2

    fail "$label"
}

expect_exec_deny() {
    local label="$1"
    local pod="$2"
    local output

    shift 2

    if output="$(
        k exec "$pod" -- "$@" \
            2>&1
    )"; then
        printf '%s\n' "$output" >&2

        fail "$label: traffic was allowed"
    fi

    pass "$label"
}

start_http_server() {
    local pod="$1"

    k exec "$pod" -- sh -ec "
        mkdir -p /tmp/setera-e2e-www

        printf 'setera-e2e\n' \
            > /tmp/setera-e2e-www/index.html

        if ! wget \
            -qO- \
            http://127.0.0.1:${HTTP_PORT}/ \
            >/dev/null 2>&1; then

            httpd \
                -p ${HTTP_PORT} \
                -h /tmp/setera-e2e-www
        fi
    " >/dev/null
}

wait_for_service_endpoints() {
    local service="$1"
    local attempt
    local addresses

    for attempt in $(seq 1 30); do
        addresses="$(
            k get endpoints "$service" \
                -o jsonpath='{.subsets[*].addresses[*].ip}' \
                2>/dev/null ||
                true
        )"

        if [[ -n "$addresses" ]]; then
            return 0
        fi

        sleep 1
    done

    fail "Service $service has no ready endpoints"
}

log "Discover test Pods"

mapfile -t A_PODS < <(
    pods_for_tenant "$TENANT_A"
)

mapfile -t B_PODS < <(
    pods_for_tenant "$TENANT_B"
)

[[ ${#A_PODS[@]} -eq 3 ]] ||
    fail \
        "expected 3 $TENANT_A Pods, got ${#A_PODS[@]}"

[[ ${#B_PODS[@]} -eq 3 ]] ||
    fail \
        "expected 3 $TENANT_B Pods, got ${#B_PODS[@]}"

k get pods \
    -l "$TENANT_LABEL" \
    -o custom-columns='POD:.metadata.name,TENANT:.metadata.labels.setera\.com/tenant,NODE:.spec.nodeName,IP:.status.podIP'

A_NODE_COUNT="$(
    unique_node_count "${A_PODS[@]}"
)"

B_NODE_COUNT="$(
    unique_node_count "${B_PODS[@]}"
)"

[[ "$A_NODE_COUNT" -eq 2 ]] ||
    fail \
        "$TENANT_A Pods must span 2 nodes, got $A_NODE_COUNT"

[[ "$B_NODE_COUNT" -eq 2 ]] ||
    fail \
        "$TENANT_B Pods must span 2 nodes, got $B_NODE_COUNT"

if ! read -r \
    A_SAME_1 \
    A_SAME_2 \
    < <(
        find_same_node_pair \
            "${A_PODS[@]}"
    ); then

    fail \
        "$TENANT_A does not have a same-node Pod pair"
fi

if ! read -r \
    B_SAME_1 \
    B_SAME_2 \
    < <(
        find_same_node_pair \
            "${B_PODS[@]}"
    ); then

    fail \
        "$TENANT_B does not have a same-node Pod pair"
fi

if ! read -r \
    A_CROSS_1 \
    A_CROSS_2 \
    < <(
        find_cross_node_pair \
            "${A_PODS[@]}"
    ); then

    fail \
        "$TENANT_A does not have a cross-node Pod pair"
fi

if ! read -r \
    B_CROSS_1 \
    B_CROSS_2 \
    < <(
        find_cross_node_pair \
            "${B_PODS[@]}"
    ); then

    fail \
        "$TENANT_B does not have a cross-node Pod pair"
fi

CROSS_TENANT_SAME_A=""
CROSS_TENANT_SAME_B=""

CROSS_TENANT_CROSS_A=""
CROSS_TENANT_CROSS_B=""

for a_pod in "${A_PODS[@]}"; do
    a_node="$(pod_node "$a_pod")"

    for b_pod in "${B_PODS[@]}"; do
        b_node="$(pod_node "$b_pod")"

        if [[
            -z "$CROSS_TENANT_SAME_A" &&
            "$a_node" == "$b_node"
        ]]; then

            CROSS_TENANT_SAME_A="$a_pod"
            CROSS_TENANT_SAME_B="$b_pod"
        fi

        if [[
            -z "$CROSS_TENANT_CROSS_A" &&
            "$a_node" != "$b_node"
        ]]; then

            CROSS_TENANT_CROSS_A="$a_pod"
            CROSS_TENANT_CROSS_B="$b_pod"
        fi
    done
done

[[ -n "$CROSS_TENANT_SAME_A" ]] ||
    fail \
        "no same-node cross-tenant Pod pair exists"

[[ -n "$CROSS_TENANT_CROSS_A" ]] ||
    fail \
        "no cross-node cross-tenant Pod pair exists"

log "Direct Pod connectivity"

expect_ping_allow \
    "$TENANT_A same node" \
    "$A_SAME_1" \
    "$A_SAME_2"

expect_ping_allow \
    "$TENANT_B same node" \
    "$B_SAME_1" \
    "$B_SAME_2"

expect_ping_allow \
    "$TENANT_A cross node" \
    "$A_CROSS_1" \
    "$A_CROSS_2"

expect_ping_allow \
    "$TENANT_B cross node" \
    "$B_CROSS_1" \
    "$B_CROSS_2"

expect_ping_deny \
    "cross tenant same node: $TENANT_A -> $TENANT_B" \
    "$CROSS_TENANT_SAME_A" \
    "$CROSS_TENANT_SAME_B"

expect_ping_deny \
    "cross tenant same node: $TENANT_B -> $TENANT_A" \
    "$CROSS_TENANT_SAME_B" \
    "$CROSS_TENANT_SAME_A"

expect_ping_deny \
    "cross tenant cross node: $TENANT_A -> $TENANT_B" \
    "$CROSS_TENANT_CROSS_A" \
    "$CROSS_TENANT_CROSS_B"

expect_ping_deny \
    "cross tenant cross node: $TENANT_B -> $TENANT_A" \
    "$CROSS_TENANT_CROSS_B" \
    "$CROSS_TENANT_CROSS_A"

log "DNS and Kubernetes Service path"

DNS_POD_IP="$(
    k -n kube-system get pods \
        -l k8s-app=kube-dns \
        -o jsonpath='{.items[0].status.podIP}'
)"

[[ -n "$DNS_POD_IP" ]] ||
    fail \
        "CoreDNS Pod IP is empty"

expect_exec_allow \
    "$TENANT_A -> CoreDNS Pod IP" \
    "${A_PODS[0]}" \
    nslookup \
    kubernetes.default.svc.cluster.local \
    "$DNS_POD_IP"

expect_exec_allow \
    "$TENANT_B -> CoreDNS Pod IP" \
    "${B_PODS[0]}" \
    nslookup \
    kubernetes.default.svc.cluster.local \
    "$DNS_POD_IP"

expect_exec_allow \
    "$TENANT_A -> kube-dns Service" \
    "${A_PODS[0]}" \
    nslookup \
    kubernetes.default.svc.cluster.local

expect_exec_allow \
    "$TENANT_B -> kube-dns Service" \
    "${B_PODS[0]}" \
    nslookup \
    kubernetes.default.svc.cluster.local

log "External network access"

expect_exec_allow \
    "$TENANT_A -> external IP $EXTERNAL_IP" \
    "${A_PODS[0]}" \
    ping \
    -c 2 \
    -W 2 \
    "$EXTERNAL_IP"

expect_exec_allow \
    "$TENANT_B -> external IP $EXTERNAL_IP" \
    "${B_PODS[0]}" \
    ping \
    -c 2 \
    -W 2 \
    "$EXTERNAL_IP"

expect_exec_allow \
    "$TENANT_A -> Internet HTTP" \
    "${A_PODS[0]}" \
    wget \
    -qO- \
    -T 5 \
    http://example.com

expect_exec_allow \
    "$TENANT_B -> Internet HTTP" \
    "${B_PODS[0]}" \
    wget \
    -qO- \
    -T 5 \
    http://example.com

log "Tenant-backed Service isolation"

for pod in "${A_PODS[@]}"; do
    start_http_server "$pod"
done

for pod in "${B_PODS[@]}"; do
    start_http_server "$pod"
done

cat <<EOF_SERVICES | k apply -f - >/dev/null
apiVersion: v1
kind: Service
metadata:
  name: ${SERVICE_A}
spec:
  selector:
    ${TENANT_LABEL}: ${TENANT_A}
  ports:
    - name: http
      port: ${HTTP_PORT}
      targetPort: ${HTTP_PORT}
      protocol: TCP
---
apiVersion: v1
kind: Service
metadata:
  name: ${SERVICE_B}
spec:
  selector:
    ${TENANT_LABEL}: ${TENANT_B}
  ports:
    - name: http
      port: ${HTTP_PORT}
      targetPort: ${HTTP_PORT}
      protocol: TCP
EOF_SERVICES

wait_for_service_endpoints "$SERVICE_A"
wait_for_service_endpoints "$SERVICE_B"

SERVICE_A_IP="$(
    k get service "$SERVICE_A" \
        -o jsonpath='{.spec.clusterIP}'
)"

SERVICE_B_IP="$(
    k get service "$SERVICE_B" \
        -o jsonpath='{.spec.clusterIP}'
)"

[[ -n "$SERVICE_A_IP" ]] ||
    fail \
        "$SERVICE_A ClusterIP is empty"

[[ -n "$SERVICE_B_IP" ]] ||
    fail \
        "$SERVICE_B ClusterIP is empty"

expect_exec_allow \
    "$TENANT_A -> $TENANT_A Service" \
    "${A_PODS[0]}" \
    wget \
    -qO- \
    -T 3 \
    "http://${SERVICE_A_IP}:${HTTP_PORT}/"

expect_exec_allow \
    "$TENANT_B -> $TENANT_B Service" \
    "${B_PODS[0]}" \
    wget \
    -qO- \
    -T 3 \
    "http://${SERVICE_B_IP}:${HTTP_PORT}/"

expect_exec_deny \
    "$TENANT_A -> $TENANT_B Service" \
    "${A_PODS[0]}" \
    wget \
    -qO- \
    -T 2 \
    "http://${SERVICE_B_IP}:${HTTP_PORT}/"

expect_exec_deny \
    "$TENANT_B -> $TENANT_A Service" \
    "${B_PODS[0]}" \
    wget \
    -qO- \
    -T 2 \
    "http://${SERVICE_A_IP}:${HTTP_PORT}/"

log "All Setera E2E connectivity tests passed"
