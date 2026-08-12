#include "common.h"

volatile const char my_tenant[64];
volatile const __u32 my_is_default;

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
    __type(key, __u32);                // Pod IPv4 address
    __type(value, struct veth_tenant); // tenant + local veth ifindex / remote marker
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_podIDs SEC(".maps");

// Keep this map in the ELF for loader compatibility. The kernel routing table
// now owns Pod forwarding, so the Pod TC program does not redirect to VXLAN.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_vxlan_ifindex SEC(".maps");

static __always_inline int is_default_tenant(char *name)
{
    if (!name)
        return 0;
    if (name[0] != 'd') return 0;
    if (name[1] != 'e') return 0;
    if (name[2] != 'f') return 0;
    if (name[3] != 'a') return 0;
    if (name[4] != 'u') return 0;
    if (name[5] != 'l') return 0;
    if (name[6] != 't') return 0;
    if (name[7] != '\0') return 0;
    return 1;
}

static __always_inline int tenant_matches(char *tenant)
{
    if (!tenant)
        return 0;

#pragma unroll
    for (int i = 0; i < 64; i++) {
        if (tenant[i] != my_tenant[i])
            return 0;
        if (tenant[i] == '\0')
            return 1;
    }

    return 1;
}

static __always_inline struct iphdr *parse_ipv4(struct __sk_buff *skb)
{
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return NULL;

    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return NULL;

    struct iphdr *iph = (void *)(eth + 1);
    if ((void *)(iph + 1) > data_end)
        return NULL;

    if (iph->ihl < 5)
        return NULL;

    return iph;
}

static __always_inline struct pkt_stats *update_stats(
    struct __sk_buff *skb,
    __u32 direction)
{
    struct pkt_stats *stats = bpf_map_lookup_elem(&tc_stats, &direction);
    if (!stats)
        return NULL;

    __u64 pkt_len = skb->len;

    if (direction == DIR_INGRESS) {
        stats->rx_packets++;
        stats->rx_bytes += pkt_len;
    } else {
        stats->tx_packets++;
        stats->tx_bytes += pkt_len;
    }

    return stats;
}

// Ingress runs on the host side of the Pod veth for traffic sent by the Pod.
// Enforce tenant policy only when the destination is a managed Pod. Unknown
// destinations, such as Service IPs and external networks, continue through
// the normal Linux routing and netfilter path.
static __always_inline int tc_ingress_core(struct __sk_buff *skb)
{
    struct iphdr *iph = parse_ipv4(skb);
    if (!iph)
        return TC_ACT_OK;

    __u32 dst_ip = iph->daddr;
    struct pkt_stats *stats = update_stats(skb, DIR_INGRESS);

    struct veth_tenant *dst_info = bpf_map_lookup_elem(
        &tc_podIDs,
        &dst_ip);
    if (!dst_info)
        return TC_ACT_OK;

    if (my_is_default || is_default_tenant(dst_info->tenant))
        return TC_ACT_OK;

    if (!tenant_matches(dst_info->tenant)) {
        if (stats)
            stats->dropped++;
        return TC_ACT_SHOT;
    }

    // Do not redirect managed Pod traffic here. The Linux routing table owns
    // local and VXLAN forwarding. This keeps kube-proxy and conntrack in the
    // packet path for Service DNAT and reverse NAT.
    return TC_ACT_OK;
}

// Egress runs on the host side of the Pod veth for traffic delivered to the
// Pod. This is the final tenant check after routing and Service DNAT.
//
// If the source is another managed Pod, compare its tenant with the tenant of
// this destination Pod. If the source is not a managed Pod, allow it. This
// permits replies from Services and external networks while blocking a Service
// that resolves to a Pod in a different tenant.
static __always_inline int tc_egress_core(struct __sk_buff *skb)
{
    struct iphdr *iph = parse_ipv4(skb);
    if (!iph)
        return TC_ACT_OK;

    __u32 src_ip = iph->saddr;
    struct pkt_stats *stats = update_stats(skb, DIR_EGRESS);

    struct veth_tenant *src_info = bpf_map_lookup_elem(
        &tc_podIDs,
        &src_ip);
    if (!src_info)
        return TC_ACT_OK;

    if (my_is_default || is_default_tenant(src_info->tenant))
        return TC_ACT_OK;

    if (!tenant_matches(src_info->tenant)) {
        if (stats)
            stats->dropped++;
        return TC_ACT_SHOT;
    }

    return TC_ACT_OK;
}

SEC("tc/ingress")
int tc_firewall_ingress(struct __sk_buff *skb)
{
    return tc_ingress_core(skb);
}

SEC("tc/egress")
int tc_firewall_egress(struct __sk_buff *skb)
{
    return tc_egress_core(skb);
}

char _license[] SEC("license") = "GPL";
