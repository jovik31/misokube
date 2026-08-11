// SPDX-License-Identifier: GPL-2.0
// XDP firewall program for Setera tenant isolation
//
// Attaches at the network driver level for fastest possible packet filtering.
// Supports per-rule allow/drop decisions with packet/byte counters.

#include "common.h"

// ---- BPF Maps ----

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32); // interface index
    __type(value, char[64]);//tenant name
} xdp_iface_action SEC(".maps");

// Firewall rules map
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_RULES);
    __type(key, struct fw_rule_key);
    __type(value, struct fw_rule_val);
} xdp_fw_rules SEC(".maps");

//LPM trie tree to block IP ranges intead of individual IPs
struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE);
    __type(key, struct lpm_v4_key);
    __type(value, struct fw_rule_val);
    __uint(map_flags, BPF_F_NO_PREALLOC); 
    __uint(max_entries, MAX_RULES);
} xdp_fw_rules_CIDR_src SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE);
    __type(key, struct lpm_v4_key);
    __type(value, struct fw_rule_val);
    __uint(map_flags, BPF_F_NO_PREALLOC); 
    __uint(max_entries, MAX_RULES);
} xdp_fw_rules_CIDR_dst SEC(".maps");

// Per-interface configuration
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct iface_config);
} xdp_iface_cfg SEC(".maps");

// Packet statistics (per-CPU for lock-free updates)
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct pkt_stats);
} xdp_stats SEC(".maps");

// ---- Helpers ----

static __always_inline struct fw_rule_val *lookup_rule(
    __u32 src_ip, __u32 dst_ip,
    __u16 src_port, __u16 dst_port,
    __u8 protocol)
{
    struct fw_rule_key key = {};
    struct fw_rule_val *val;

    // Exact 5-tuple match
    key.src_ip    = src_ip;
    key.dst_ip    = dst_ip;
    key.src_port  = src_port;
    key.dst_port  = dst_port;
    key.protocol  = protocol;
    key.src_prefix = 32;
    key.dst_prefix = 32;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Exact 5-tuple, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Match source/dest IP only (any port)
    key.src_port = 0;
    key.dst_port = 0;
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // IP pair only, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Match destination IP + port (any source)
    key.src_ip   = 0;
    key.src_prefix = 0;
    key.dst_ip   = dst_ip;
    key.dst_port = dst_port;
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Dst IP + port, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Match destination IP only
    key.dst_port = 0;
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Dst IP only, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Match source IP only
    key.src_ip     = src_ip;
    key.src_prefix = 32;
    key.dst_ip     = 0;
    key.dst_prefix = 0;
    key.protocol   = protocol;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // Src IP only, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    // CIDR source lookup (longest-prefix match)
    struct lpm_v4_key src_lpm = {
        .prefixlen = 32,
        .addr = src_ip,
    };
    val = bpf_map_lookup_elem(&xdp_fw_rules_CIDR_src, &src_lpm);
    if (val)
        return val;

    // CIDR destination lookup (longest-prefix match)
    struct lpm_v4_key dst_lpm = {
        .prefixlen = 32,
        .addr = dst_ip,
    };
    val = bpf_map_lookup_elem(&xdp_fw_rules_CIDR_dst, &dst_lpm);
    if (val)
        return val;

    // Match protocol only
    __builtin_memset(&key, 0, sizeof(key));
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&xdp_fw_rules, &key);
    if (val)
        return val;

    return NULL;
}

// ---- XDP Program ----

SEC("xdp")
int xdp_firewall(struct xdp_md *ctx)
{
    void *data     = (void *)(long)ctx->data;
    void *data_end = (void *)(long)ctx->data_end;
    __u32 ifindex = ctx->ingress_ifindex;

    // Parse Ethernet header
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return XDP_PASS;

    // Only handle IPv4
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return XDP_PASS;

    // Parse IP header
    struct iphdr *iph = (void *)(eth + 1);
    if ((void *)(iph + 1) > data_end)
        return XDP_PASS;

    if (iph->ihl < 5)
        return XDP_PASS;

    // Parse L4
    __u16 src_port = 0, dst_port = 0;
    if (parse_l4(data, data_end, iph, &src_port, &dst_port) < 0)
        return XDP_PASS;

    // Update stats
    __u32 stats_key = 0;
    struct pkt_stats *stats = bpf_map_lookup_elem(&xdp_stats, &stats_key);
    if (stats) {
        stats->rx_packets++;
        stats->rx_bytes += (data_end - data);
    }

    // Lookup firewall rule
    struct fw_rule_val *rule = lookup_rule(
        iph->saddr, iph->daddr,
        src_port, dst_port,
        iph->protocol);

    if (rule) {
        // Update rule counters
        __sync_fetch_and_add(&rule->packets, 1);
        __sync_fetch_and_add(&rule->bytes, (data_end - data));

        if (rule->action == FW_ACTION_DROP) {
            if (stats)
                stats->dropped++;
            return XDP_DROP;
        }

        if (rule->action == FW_ACTION_LOG) {
            bpf_printk("xdp_fw: ALLOW+LOG src=%pI4 dst=%pI4 proto=%d",
                        &iph->saddr, &iph->daddr, iph->protocol);
        }

        return XDP_PASS;
    }

    // No rule matched: check default action
    __u32 cfg_key = 0;
    struct iface_config *cfg = bpf_map_lookup_elem(&xdp_iface_cfg, &cfg_key);
    if (cfg && cfg->default_action == FW_ACTION_DROP) {
        if (stats)
            stats->dropped++;
        return XDP_DROP;
    }

    return XDP_PASS;
}

char _license[] SEC("license") = "GPL";
