#include "common.h"

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct iface_config);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_iface_cfg SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 2);
    __type(key, __u32); // direction index
    __type(value, struct pkt_stats);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_stats SEC(".maps");

// Keep the shared Pod map in the node program for loader compatibility and
// map ownership checks. Linux routing now forwards decapsulated Pod traffic.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32);                // Pod IPv4 address
    __type(value, struct veth_tenant); // tenant + local veth ifindex / remote marker
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_podIDs SEC(".maps");

// The VXLAN ingress program must not redirect directly to a Pod veth.
// Returning TC_ACT_OK keeps netfilter and conntrack in the packet path. This
// is required for kube-proxy reverse NAT when a Service backend is on another
// node.
static __always_inline int tc_node_core(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;

    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    struct iphdr *iph = (void *)(eth + 1);
    if ((void *)(iph + 1) > data_end)
        return TC_ACT_OK;

    if (iph->ihl < 5)
        return TC_ACT_OK;

    __u32 stats_key = DIR_INGRESS;
    struct pkt_stats *stats = bpf_map_lookup_elem(&tc_stats, &stats_key);
    if (stats) {
        stats->rx_packets++;
        stats->rx_bytes += skb->len;
    }

    return TC_ACT_OK;
}

SEC("tc/ingress")
int tc_node_ingress(struct __sk_buff *skb)
{
    return tc_node_core(skb);
}

SEC("tc/egress")
int tc_node_egress(struct __sk_buff *skb)
{
    (void)skb;
    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";
