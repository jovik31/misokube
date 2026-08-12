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
    __type(key, __u32);              // Pod IPv4 address
    __type(value, struct veth_tenant); // tenant + local veth ifindex / remote marker
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_podIDs SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32); // node-wide VXLAN ifindex
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

static __always_inline int redirect_offnode_neigh(void)
{
    __u32 key = 0;
    __u32 *vx_ifindex = bpf_map_lookup_elem(&tc_vxlan_ifindex, &key);
    if (!vx_ifindex || *vx_ifindex == 0)
        return TC_ACT_SHOT;

    return bpf_redirect_neigh(*vx_ifindex, NULL, 0, 0);
}

static __always_inline int redirect_local_peer(__u32 dst_ifindex)
{
    return bpf_redirect_peer(dst_ifindex, 0);
}

static __always_inline int tc_firewall_core(struct __sk_buff *skb)
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

    // Current Setera routing/isolation depends only on destination IPv4 and
    // tenant identity. Avoid parsing TCP/UDP/ICMP on every packet.

    __u32 stats_key = DIR_INGRESS;
    __u32 pkt_len = data_end - data;
    struct pkt_stats *stats = bpf_map_lookup_elem(&tc_stats, &stats_key);
    if (stats) {
        stats->rx_packets++;
        stats->rx_bytes += pkt_len;
    }

    // tc_iface_cfg is intentionally not consulted on the hot path. Its
    // current conntrack/default-action values do not participate in Setera's
    // identity routing decision.

    struct veth_tenant *dst_info = bpf_map_lookup_elem(&tc_podIDs, &iph->daddr);
    if (!dst_info)
        return TC_ACT_OK;

    // my_is_default is a loader-populated scalar. Keep the source-default
    // decision as an explicit volatile load so Clang cannot constant-fold the
    // source tenant check from the initial .rodata contents.
    if (!my_is_default && !is_default_tenant(dst_info->tenant)) {
        for (int i = 0; i < 64; i++) {
            if (dst_info->tenant[i] != my_tenant[i]) {
                if (stats)
                    stats->dropped++;
                return TC_ACT_SHOT;
            }
            if (dst_info->tenant[i] == '\0')
                break;
        }
    }

    if (dst_info->veth_ifindex == (__u32)-1)
        return redirect_offnode_neigh();

    return redirect_local_peer(dst_info->veth_ifindex);
}

SEC("tc/ingress")
int tc_firewall_ingress(struct __sk_buff *skb)
{
    return tc_firewall_core(skb);
}

// Kept in the ELF for compatibility with existing generated bindings, but the
// loader no longer attaches an egress TC filter. This program therefore has no
// packet-path cost.
SEC("tc/egress")
int tc_firewall_egress(struct __sk_buff *skb)
{
    (void)skb;
    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";
