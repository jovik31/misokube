package resolver

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// PodKey builds the canonical "ns/pod" key.
func PodKey(ns, pod string) string { return ns + "/" + pod }

// ParseK8sArgs parses "K8S_POD_NAME=...;K8S_POD_NAMESPACE=...;K8S_POD_UID=..."
func ParseK8sArgs(args string) (ns, pod, uid string) {
	for _, kv := range strings.Split(args, ";") {
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "K8S_POD_NAME":
			pod = parts[1]
		case "K8S_POD_NAMESPACE":
			ns = parts[1]
		case "K8S_POD_UID":
			uid = parts[1]
		}
	}
	return
}

func tenantFromLabels(lbls map[string]string, key string) string {
	if key == "" || lbls == nil {
		return ""
	}
	return strings.TrimSpace(lbls[key])
}

// tenantFromPod attempts to derive the tenant from Pod labels and annotations
// using the provided key and common aliases. It tries, in order:
// - exact key in labels
// - suffix after '/' of the key (e.g., "tenant" from "setera.com/tenant") in labels
// - common alias keys in labels
// - exact key in annotations, then suffix, then aliases
func tenantFromPod(p *corev1.Pod, key string) string {
	if p == nil {
		return ""
	}
	// Build candidate keys
	candidates := make([]string, 0, 6)
	if key != "" {
		candidates = append(candidates, key)
		if i := strings.IndexByte(key, '/'); i >= 0 && i+1 < len(key) {
			candidates = append(candidates, key[i+1:])
		}
	}
	// Add common aliases
	aliases := []string{
		"setera.com/tenant",
		"tenant",
		"tenantId",
		"tenant-id",
		"tenant_name",
	}
	candidates = append(candidates, aliases...)

	// Search labels first
	for _, k := range candidates {
		if v := strings.TrimSpace(p.Labels[k]); v != "" {
			return v
		}
	}
	// Then annotations
	for _, k := range candidates {
		if v := strings.TrimSpace(p.Annotations[k]); v != "" {
			return v
		}
	}
	return ""
}
