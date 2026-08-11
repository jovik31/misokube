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

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32);              // Pod IPv4 address
    __type(value, struct veth_tenant); // tenant + local veth ifindex / remote marker
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_podIDs SEC(".maps");

// The node/VXLAN program is forwarding-only. Tenant isolation is enforced by
// the source Pod's tc_router program before remote traffic enters the overlay.
static __always_inline int tc_node_core(struct __sk_buff *skb)
{
    void *data     = (void *)(long)skb->data;
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

    // No L4 parsing is needed: VXLAN ingress only needs the destination Pod IP.
    __u32 stats_key = DIR_INGRESS;
    __u32 pkt_len = data_end - data;
    struct pkt_stats *stats = bpf_map_lookup_elem(&tc_stats, &stats_key);
    if (stats) {
        stats->rx_packets++;
        stats->rx_bytes += pkt_len;
    }

    // tc_iface_cfg is not part of the forwarding decision and is intentionally
    // skipped on the hot path.
    struct veth_tenant *dst_info = bpf_map_lookup_elem(&tc_podIDs, &iph->daddr);
    if (!dst_info)
        return TC_ACT_OK;

    if (dst_info->veth_ifindex == (__u32)-1)
        return TC_ACT_OK;

    return bpf_redirect_peer(dst_info->veth_ifindex, 0);
}

SEC("tc/ingress")
int tc_node_ingress(struct __sk_buff *skb)
{
    return tc_node_core(skb);
}

// Kept in the ELF for compatibility with existing generated bindings, but the
// loader no longer attaches an egress TC filter. This program therefore has no
// packet-path cost.
SEC("tc/egress")
int tc_node_egress(struct __sk_buff *skb)
{
    (void)skb;
    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";