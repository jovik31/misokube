#include "common.h"

volatile const char my_tenant[64];

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
    __type(key, __u32);
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

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32); // vxlan ifindex
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

static __always_inline int redirect_offnode_neigh(struct __sk_buff *skb, struct iphdr *iph)
{
    __u32 key = 0;
    __u32 *vx_ifindex = bpf_map_lookup_elem(&tc_vxlan_ifindex, &key);
    if (!vx_ifindex || *vx_ifindex == 0) {
        //bpf_printk("tc_router: vxlan ifindex missing src=%x dst=%x\n", iph->saddr, iph->daddr);
        return TC_ACT_OK;
    }
    return bpf_redirect_neigh(*vx_ifindex, NULL, 0, 0);
}

static __always_inline int redirect_local_peer(struct __sk_buff *skb, __u32 dst_ifindex)
{
    (void)skb;
    return bpf_redirect_peer(dst_ifindex, 0);
}

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

     // Load interface config
    __u32 cfg_key = 0;
    struct iface_config *cfg = bpf_map_lookup_elem(&tc_iface_cfg, &cfg_key);
    int conntrack_enabled = cfg && (cfg->flags & 1);

    struct veth_tenant *dst_info = bpf_map_lookup_elem(&tc_podIDs, &iph->daddr);

    if (dst_info) {
        //bpf_printk("tc_router: src=%x dst=%x ifindex=%u\n", iph->saddr, iph->daddr, dst_info->veth_ifindex);
        if (!is_default_tenant((char *)my_tenant) && !is_default_tenant(dst_info->tenant)) {
            for (int i = 0; i < 64; i++) {
                if (dst_info->tenant[i] != my_tenant[i]) {
                    if (stats)
                        stats->dropped++;
                    //bpf_printk("tc_router: drop tenant mismatch src=%x dst=%x\n", iph->saddr, iph->daddr);
                    return TC_ACT_SHOT;
                }
                if (dst_info->tenant[i] == '\0')
                    break;
            }
        }
        if (dst_info->veth_ifindex == (__u32)-1) {
            //bpf_printk("tc_router: remote src=%x dst=%x\n", iph->saddr, iph->daddr);
            return redirect_offnode_neigh(skb, iph);
        }
        //bpf_printk("tc_router: local src=%x dst=%x ifindex=%u\n", iph->saddr, iph->daddr, dst_info->veth_ifindex);
        return redirect_local_peer(skb, dst_info->veth_ifindex);
    }

    return TC_ACT_OK;

}


SEC("tc/ingress")
int tc_firewall_ingress(struct __sk_buff *skb)
{
    return tc_firewall_core(skb, DIR_INGRESS);
}

SEC("tc/egress")
int tc_firewall_egress(struct __sk_buff *skb)
{
    // Routing/isolation decisions are enforced on ingress.
    // Keeping egress passive avoids veth recirculation artifacts.
    (void)skb;
    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";