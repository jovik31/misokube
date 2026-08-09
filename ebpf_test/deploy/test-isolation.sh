#!/bin/bash
#
# eBPF Firewall Isolation Test
#
# Scenario:
#   - pod-a and pod-b are BLOCKED from communicating with each other
#   - pod-c can communicate with BOTH pod-a and pod-b
#
# Steps:
#   1. Install CNI (kindnet) if not present
#   2. Deploy 3 test pods
#   3. Discover pod IPs
#   4. Generate firewall rules and deploy as ConfigMap
#   5. Deploy/restart the firewall DaemonSet
#   6. Run connectivity tests
#
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$SCRIPT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info()  { echo -e "${YELLOW}[INFO]${NC} $*"; }
pass()  { echo -e "${GREEN}[PASS]${NC} $*"; }
fail()  { echo -e "${RED}[FAIL]${NC} $*"; }


# ---- Step 1: Deploy test pods ----
info "Deploying test pods (pod-a, pod-b, pod-c)..."
kubectl apply -f "$DEPLOY_DIR/test-pods.yaml"

info "Waiting for pods to be ready..."
kubectl wait --for=condition=Ready pod/pod-a pod/pod-b pod/pod-c --timeout=120s

make deploy
# ---- Step 2: Get pod IPs ----
POD_A_IP=$(kubectl get pod pod-a -o jsonpath='{.status.podIP}')
POD_B_IP=$(kubectl get pod pod-b -o jsonpath='{.status.podIP}')
POD_C_IP=$(kubectl get pod pod-c -o jsonpath='{.status.podIP}')

info "Pod IPs:"
info "  pod-a: $POD_A_IP"
info "  pod-b: $POD_B_IP"
info "  pod-c: $POD_C_IP"

if [[ -z "$POD_A_IP" || -z "$POD_B_IP" || -z "$POD_C_IP" ]]; then
    fail "Could not get all pod IPs. Is the CNI working?"
    exit 1
fi

# ---- Step 3: Verify baseline (all pods can communicate) ----
info "=== BASELINE TEST (before firewall rules) ==="

info "Testing pod-a -> pod-b..."
kubectl exec pod-a -- wget -qO- -T 3 http://$POD_B_IP:8080 && pass "pod-a -> pod-b: OK" || fail "pod-a -> pod-b: FAILED (baseline broken)"

info "Testing pod-a -> pod-c..."
kubectl exec pod-a -- wget -qO- -T 3 http://$POD_C_IP:8080 && pass "pod-a -> pod-c: OK" || fail "pod-a -> pod-c: FAILED (baseline broken)"

info "Testing pod-b -> pod-c..."
kubectl exec pod-b -- wget -qO- -T 3 http://$POD_C_IP:8080 && pass "pod-b -> pod-c: OK" || fail "pod-b -> pod-c: FAILED (baseline broken)"

# ---- Step 4: Create firewall rules ----
# Block pod-a <-> pod-b (both directions)
# Allow everything else (default=allow)
info "Creating firewall rules to block pod-a <-> pod-b..."

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: firewall-rules
data:
  rules.json: |
    [
      {"srcIP": "$POD_A_IP", "dstIP": "$POD_B_IP", "action": "drop"},
      {"srcIP": "$POD_B_IP", "dstIP": "$POD_A_IP", "action": "drop"}
    ]
EOF

# ---- Step 5: Deploy/restart firewall ----
info "Deploying RBAC and firewall DaemonSet..."
kubectl apply -f "$DEPLOY_DIR/rbac.yaml"
kubectl apply -f "$DEPLOY_DIR/daemonset.yaml"

info "Waiting for firewall pods to be ready..."
# Give the BPF programs a moment to attach
sleep 3

# ---- Step 7: Test with firewall active ----
info ""
info "=== ISOLATION TEST (firewall active) ==="
info "Expected: pod-a <-> pod-b BLOCKED, pod-c can talk to both"
info ""

PASSED=0
TOTAL=0

# pod-a -> pod-b: should FAIL
TOTAL=$((TOTAL + 1))
info "Testing pod-a -> pod-b (expect: BLOCKED)..."
if kubectl exec pod-a -- wget -qO- -T 3 http://$POD_B_IP:8080 2>/dev/null; then
    fail "pod-a -> pod-b: ALLOWED (should be blocked!)"
else
    pass "pod-a -> pod-b: BLOCKED"
    PASSED=$((PASSED + 1))
fi

# pod-b -> pod-a: should FAIL
TOTAL=$((TOTAL + 1))
info "Testing pod-b -> pod-a (expect: BLOCKED)..."
if kubectl exec pod-b -- wget -qO- -T 3 http://$POD_A_IP:8080 2>/dev/null; then
    fail "pod-b -> pod-a: ALLOWED (should be blocked!)"
else
    pass "pod-b -> pod-a: BLOCKED"
    PASSED=$((PASSED + 1))
fi

# pod-a -> pod-c: should PASS
TOTAL=$((TOTAL + 1))
info "Testing pod-a -> pod-c (expect: ALLOWED)..."
if kubectl exec pod-a -- wget -qO- -T 3 http://$POD_C_IP:8080 2>/dev/null; then
    pass "pod-a -> pod-c: ALLOWED"
    PASSED=$((PASSED + 1))
else
    fail "pod-a -> pod-c: BLOCKED (should be allowed!)"
fi

# pod-c -> pod-a: should PASS
TOTAL=$((TOTAL + 1))
info "Testing pod-c -> pod-a (expect: ALLOWED)..."
if kubectl exec pod-c -- wget -qO- -T 3 http://$POD_A_IP:8080 2>/dev/null; then
    pass "pod-c -> pod-a: ALLOWED"
    PASSED=$((PASSED + 1))
else
    fail "pod-c -> pod-a: BLOCKED (should be allowed!)"
fi

# pod-b -> pod-c: should PASS
TOTAL=$((TOTAL + 1))
info "Testing pod-b -> pod-c (expect: ALLOWED)..."
if kubectl exec pod-b -- wget -qO- -T 3 http://$POD_C_IP:8080 2>/dev/null; then
    pass "pod-b -> pod-c: ALLOWED"
    PASSED=$((PASSED + 1))
else
    fail "pod-b -> pod-c: BLOCKED (should be allowed!)"
fi

# pod-c -> pod-b: should PASS
TOTAL=$((TOTAL + 1))
info "Testing pod-c -> pod-b (expect: ALLOWED)..."
if kubectl exec pod-c -- wget -qO- -T 3 http://$POD_B_IP:8080 2>/dev/null; then
    pass "pod-c -> pod-b: ALLOWED"
    PASSED=$((PASSED + 1))
else
    fail "pod-c -> pod-b: BLOCKED (should be allowed!)"
fi

# ---- Summary ----
info ""
info "=== RESULTS: $PASSED/$TOTAL tests passed ==="
if [[ $PASSED -eq $TOTAL ]]; then
    pass "All tests passed! eBPF firewall isolation is working correctly."
else
    fail "$((TOTAL - PASSED)) test(s) failed."
fi
