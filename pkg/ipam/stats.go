package ipam

// Stats describe IPAM utilization.
//
// Capacity is the number of IP addresses available for allocation after
// accounting for reserved IP addresses and already allocated IP addresses.
type Stats struct {
	Capacity  uint64
	Allocated uint64
	Available uint64
}
