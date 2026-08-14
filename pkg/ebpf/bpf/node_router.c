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
    __type(key, __u32); //IP address
    __type(value, struct pkt_stats);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_stats SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32);      // IP address of a pod
    __type(value, struct veth_tenant); // tenant name + veth ifindex
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_podIDs SEC(".maps");

static __always_inline int tc_firewall_core(struct __sk_buff *skb, __u32 direction)
{
    void *data     = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    // Parse Ethernet header
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;

    // Only handle IPv4
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    // Parse IP header
    struct iphdr *iph = (void *)(eth + 1);
    if ((void *)(iph + 1) > data_end)
        return TC_ACT_OK;

    if (iph->ihl < 5)
        return TC_ACT_OK;

    // Parse L4
    __u16 src_port = 0, dst_port = 0;
    if (parse_l4(data, data_end, iph, &src_port, &dst_port) < 0)
        return TC_ACT_OK;

    __u32 pkt_len = data_end - data;

    // Update stats
    struct pkt_stats *stats = bpf_map_lookup_elem(&tc_stats, &direction);
    if (stats) {
        if (direction == DIR_INGRESS) {
            stats->rx_packets++;
            stats->rx_bytes += pkt_len;
        } else {
            stats->tx_packets++;
            stats->tx_bytes += pkt_len;
        }
    }

    struct veth_tenant *dst_info = bpf_map_lookup_elem(&tc_podIDs, &iph->daddr);

    if (!dst_info || dst_info->veth_ifindex == (__u32)-1)
        return TC_ACT_OK;

    // Enter the veth peer's network namespace directly. A plain
    // bpf_redirect() targets host-veth egress and retains the VTEP MAC.
    return bpf_redirect_peer(dst_info->veth_ifindex, 0);
}

SEC("tc/ingress")
int tc_node_ingress(struct __sk_buff *skb)
{
    return tc_firewall_core(skb, DIR_INGRESS);
}

SEC("tc/egress")
int tc_node_egress(struct __sk_buff *skb)
{
    return tc_firewall_core(skb, DIR_INGRESS);
}

char _license[] SEC("license") = "GPL";
