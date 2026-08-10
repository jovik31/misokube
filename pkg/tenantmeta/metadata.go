package tenantmeta

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// PodTenantLabel identifies the tenant that owns a Pod.
	PodTenantLabel = "setera.com/tenant"

	// NodeTenantLabelPrefix is the label-name prefix used for Node tenant
	// membership. The tenant name is appended to this prefix.
	NodeTenantLabelPrefix = "setera.com/tenant."

	// NodeVTEPReadyLabel reports that the node-local daemon published valid VTEP
	// metadata for the Node.
	NodeVTEPReadyLabel = "setera.com/vtep-ready"

	// NodeVTEPIPAnnotation stores the node VTEP IP address.
	NodeVTEPIPAnnotation = "setera.com/vtep-ip"

	// NodeVTEPMACAnnotation stores the node VTEP MAC address.
	NodeVTEPMACAnnotation = "setera.com/vtep-mac"
)

// VTEP contains node-to-node tunnel endpoint metadata published on a Node.
type VTEP struct {
	IP  netip.Addr
	MAC net.HardwareAddr
}

// NodeTenantLabel returns the Node label key used to represent membership of
// one tenant on a Node.
func NodeTenantLabel(tenant string) (string, error) {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return "", fmt.Errorf("tenant metadata: tenant name is empty")
	}

	key := NodeTenantLabelPrefix + tenant
	if errs := validation.IsQualifiedName(key); len(errs) != 0 {
		return "", fmt.Errorf("tenant metadata: invalid tenant name %q: %s", tenant, strings.Join(errs, "; "))
	}

	return key, nil
}

// HasTenant reports whether labels contain membership for tenant.
func HasTenant(labels map[string]string, tenant string) bool {
	key, err := NodeTenantLabel(tenant)
	if err != nil {
		return false
	}

	return labels[key] == "true"
}

// Tenants returns all Setera tenant names represented by Node membership
// labels. The returned list is sorted.
func Tenants(labels map[string]string) []string {
	out := make([]string, 0)

	for key, value := range labels {
		if value != "true" || !strings.HasPrefix(key, NodeTenantLabelPrefix) {
			continue
		}

		tenant := strings.TrimPrefix(key, NodeTenantLabelPrefix)
		if tenant == "" {
			continue
		}

		out = append(out, tenant)
	}

	sort.Strings(out)
	return out
}

// TenantCount returns the number of Setera tenant memberships on a Node.
func TenantCount(labels map[string]string) int {
	count := 0
	for key, value := range labels {
		if value == "true" && strings.HasPrefix(key, NodeTenantLabelPrefix) {
			count++
		}
	}
	return count
}

// VTEPFromMetadata reads VTEP metadata from Node labels and annotations.
// ready is false when the daemon has not published a ready VTEP yet.
func VTEPFromMetadata(labels, annotations map[string]string) (vtep VTEP, ready bool, err error) {
	if labels[NodeVTEPReadyLabel] != "true" {
		return VTEP{}, false, nil
	}

	ipText := annotations[NodeVTEPIPAnnotation]
	macText := annotations[NodeVTEPMACAnnotation]
	if ipText == "" || macText == "" {
		return VTEP{}, false, fmt.Errorf("tenant metadata: VTEP is ready but IP or MAC annotation is missing")
	}

	ip, err := netip.ParseAddr(ipText)
	if err != nil {
		return VTEP{}, false, fmt.Errorf("tenant metadata: parse VTEP IP %q: %w", ipText, err)
	}
	if ip.Zone() != "" {
		return VTEP{}, false, fmt.Errorf("tenant metadata: VTEP IP %q must not contain a zone", ipText)
	}

	mac, err := net.ParseMAC(macText)
	if err != nil {
		return VTEP{}, false, fmt.Errorf("tenant metadata: parse VTEP MAC %q: %w", macText, err)
	}

	return VTEP{IP: ip.Unmap(), MAC: mac}, true, nil
}

// VTEPNodeMetadata returns the Node label and annotations that a daemon should
// publish after it creates its local VTEP.
func VTEPNodeMetadata(vtep VTEP) (map[string]string, map[string]string, error) {
	if !vtep.IP.IsValid() || vtep.IP.Zone() != "" {
		return nil, nil, fmt.Errorf("tenant metadata: invalid VTEP IP")
	}
	if len(vtep.MAC) == 0 {
		return nil, nil, fmt.Errorf("tenant metadata: invalid VTEP MAC")
	}

	labels := map[string]string{
		NodeVTEPReadyLabel: "true",
	}
	annotations := map[string]string{
		NodeVTEPIPAnnotation:  vtep.IP.Unmap().String(),
		NodeVTEPMACAnnotation: vtep.MAC.String(),
	}

	return labels, annotations, nil
}
