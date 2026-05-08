.PHONY: help generate build image node-image cluster cluster-delete load-image deploy undeploy clean test test-router test-router-clean

SHELL := /bin/bash

# Variables
CLUSTER_NAME   ?= setera-ebpf
NODE_IMAGE     ?= kind-node-ebpf:v1.33.1
FW_IMAGE       ?= setera-ebpf-firewall:dev
CLUSTER_CONFIG ?= ../config/cluster/kind_cluster_ebpf.yaml

##@ Help

help: ## Show this help
	@awk ' \
		/^##@/ { section = substr($$0, 5); printf "\n%s\n", section; } \
		/^[a-zA-Z0-9_-]+:.*? ##/ { \
			split($$0,a,":"); target = a[1]; \
			match($$0, /## (.*)/, m); desc = m[1]; \
			printf "  %-20s %s\n", target, desc; \
		}' $(MAKEFILE_LIST)

##@ Code Generation

generate: ## Generate BPF Go bindings (requires clang, libbpf-dev)
	go generate ./pkg/firewall/...

##@ Build

build: generate ## Build the firewall binary (Linux/amd64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/firewall ./cmd/firewall/main.go

image: ## Build the firewall container image
	docker build --no-cache -t $(FW_IMAGE) .

##@ Kind Cluster

node-image: ## Build custom Kind node image with eBPF tools
	docker build -t $(NODE_IMAGE) -f Dockerfile.kindnode .

cluster: node-image ## Create the eBPF Kind cluster (builds node image first)
	kind create cluster --name=$(CLUSTER_NAME) --config=$(CLUSTER_CONFIG)

cluster-delete: ## Delete the eBPF Kind cluster
	kind delete cluster --name=$(CLUSTER_NAME)

cluster-status: ## Show cluster nodes and pods
	kubectl get nodes -o wide --context kind-$(CLUSTER_NAME)
	@echo "---"
	kubectl get pods -A --context kind-$(CLUSTER_NAME)

load-image: image ## Load firewall image into Kind cluster
	kind load docker-image $(FW_IMAGE) --name=$(CLUSTER_NAME)

##@ Deploy

deploy: load-image ## Deploy firewall DaemonSet to the cluster
	kubectl apply -f deploy/daemonset.yaml --context kind-$(CLUSTER_NAME)

undeploy: ## Remove firewall DaemonSet from the cluster
	kubectl delete -f deploy/daemonset.yaml --context kind-$(CLUSTER_NAME) --ignore-not-found

##@ Testing

test-isolation: image load-image ## Run the full isolation test (install CNI, deploy pods, apply rules, verify)
	chmod +x deploy/test-isolation.sh deploy/install-cni.sh
	bash deploy/test-isolation.sh

test-pods: ## Deploy just the 3 test pods (requires CNI)
	kubectl apply -f deploy/test-pods.yaml
	kubectl wait --for=condition=Ready pod/pod-a pod/pod-b pod/pod-c --timeout=120s
	@echo "Pod IPs:"
	@kubectl get pods -l app=test-pod -o wide

install-cni: ## Install kindnet CNI for pod networking
	chmod +x deploy/install-cni.sh
	bash deploy/install-cni.sh

test-ping: ## Run a quick cross-node ping test (requires 2 pods on different nodes)
	@echo "Creating test pods..."
	kubectl run test-ping-1 --image=busybox --restart=Never --overrides='{"spec":{"nodeSelector":{"kubernetes.io/hostname":"$(CLUSTER_NAME)-worker"}}}' -- sleep 3600 --context kind-$(CLUSTER_NAME) 2>/dev/null || true
	kubectl run test-ping-2 --image=busybox --restart=Never --overrides='{"spec":{"nodeSelector":{"kubernetes.io/hostname":"$(CLUSTER_NAME)-worker2"}}}' -- sleep 3600 --context kind-$(CLUSTER_NAME) 2>/dev/null || true
	@echo "Waiting for pods to be ready..."
	kubectl wait --for=condition=Ready pod/test-ping-1 pod/test-ping-2 --timeout=60s --context kind-$(CLUSTER_NAME)
	@echo "Getting pod IPs..."
	@POD2_IP=$$(kubectl get pod test-ping-2 -o jsonpath='{.status.podIP}' --context kind-$(CLUSTER_NAME)); \
	echo "Pinging $$POD2_IP from test-ping-1..."; \
	kubectl exec test-ping-1 --context kind-$(CLUSTER_NAME) -- ping -c 3 $$POD2_IP

test-router: ## Compile and attach tc_router to pod veth ingress, then test redirect path
	chmod +x deploy/test-tc-router.sh
	bash deploy/test-tc-router.sh

test-router-clean: ## Remove tc_router test pods
	kubectl delete -f deploy/test-pods-router.yaml --ignore-not-found --context kind-$(CLUSTER_NAME)

test-clean: ## Remove test pods and firewall rules ConfigMap
	kubectl delete pod test-ping-1 test-ping-2 pod-a pod-b pod-c --ignore-not-found --context kind-$(CLUSTER_NAME)
	kubectl delete configmap firewall-rules --ignore-not-found --context kind-$(CLUSTER_NAME)

bpf-debug: ## Show loaded BPF programs on worker node
	docker exec $(CLUSTER_NAME)-worker bpftool prog list 2>/dev/null || \
	docker exec $(CLUSTER_NAME)-worker cat /proc/kallsyms | grep -i bpf | head -20

bpf-maps: ## Show BPF maps on worker node
	docker exec $(CLUSTER_NAME)-worker bpftool map list 2>/dev/null || echo "bpftool not available"

bpf-trace: ## Show BPF trace output (bpf_printk) from worker node
	docker exec $(CLUSTER_NAME)-worker cat /sys/kernel/debug/tracing/trace_pipe

##@ Cleanup

clean: test-clean undeploy ## Clean test pods and undeploy
	rm -rf bin/

nuke: cluster-delete clean ## Delete everything (cluster + artifacts)
	docker rmi $(FW_IMAGE) $(NODE_IMAGE) 2>/dev/null || true
