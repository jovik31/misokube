#!/bin/bash
# Install kindnet CNI so pods can get IPs.
# This is the default CNI for Kind clusters — we disabled it in the config
# to have full control, but now we need basic pod networking for testing.
set -e

echo "Installing kindnet CNI..."
kubectl apply -f https://raw.githubusercontent.com/aojea/kindnet/main/install-kindnet.yaml

echo "Waiting for kindnet pods to be ready..."
kubectl -n kube-system wait --for=condition=Ready pods -l app=kindnet --timeout=60s

echo "kindnet CNI installed. Pods can now get IPs."
