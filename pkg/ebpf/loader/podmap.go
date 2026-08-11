package loader

import (
	"bytes"
	"fmt"
	"net"
	"sort"
)

// PodTenantVeth is one decoded tc_podIDs entry.
//
// This type is intentionally a loader detail. Higher layers should use the
// pkg/ebpf PodEndpoint representation instead.
type PodTenantVeth struct {
	IP      net.IP
	Tenant  string
	IfIndex int
}

// ListPodTenantVeth returns a snapshot of every entry in the shared
// tc_podIDs map.
func ListPodTenantVeth() ([]PodTenantVeth, error) {
	m, err := openTcPodIDsMap()
	if err != nil {
		return nil, err
	}
	defer m.Close()

	out := make([]PodTenantVeth, 0)

	it := m.Iterate()
	var key uint32
	var value tcPodIDValue

	for it.Next(&key, &value) {
		tenantBytes := value.Tenant[:]
		if end := bytes.IndexByte(tenantBytes, 0); end >= 0 {
			tenantBytes = tenantBytes[:end]
		}

		ifIndex := int(value.VethIfindex)
		if value.VethIfindex == ^uint32(0) {
			ifIndex = -1
		}

		out = append(out, PodTenantVeth{
			IP:      append(net.IP(nil), u32ToIP(key)...),
			Tenant:  string(tenantBytes),
			IfIndex: ifIndex,
		})
	}

	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("iterate tc_podIDs: %w", err)
	}

	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].IP.To4(), out[j].IP.To4()) < 0
	})

	return out, nil
}
