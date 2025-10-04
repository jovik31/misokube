package resolver

import "strings"

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
