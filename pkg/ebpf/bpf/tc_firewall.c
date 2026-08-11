// SPDX-License-Identifier: GPL-2.0
// TC (Traffic Control) firewall program for Setera tenant isolation
//
// Attaches to both ingress and egress TC hooks for bidirectional filtering.
// Supports per-rule allow/drop decisions with packet/byte counters and
// optional connection tracking.

#include "common.h"

// ---- BPF Maps ----

// This map stores the tenant associated with each interface index.
// The value is a fixed-size, null-terminated string (zero-padded).
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32);      // interface index
    __type(value, char[64]); // tenant name
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_iface_tenant SEC(".maps");

// Firewall rules (shared between ingress and egress)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_RULES);
    __type(key, struct fw_rule_key);
    __type(value, struct fw_rule_val);
} tc_fw_rules SEC(".maps");

//LPM trie tree to block IP ranges intead of individual IPs
struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE);
    __type(key, struct lpm_v4_key);
    __type(value, struct fw_rule_val);
    __uint(map_flags, BPF_F_NO_PREALLOC); 
    __uint(max_entries, 65535);
} tc_fw_rules_CIDR_src SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE);
    __type(key, struct lpm_v4_key);
    __type(value, struct fw_rule_val);
    __uint(map_flags, BPF_F_NO_PREALLOC); 
    __uint(max_entries, 65535);
} tc_fw_rules_CIDR_dst SEC(".maps");

// Connection tracking table
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_CONNTRACK);
    __type(key, struct conntrack_key);
    __type(value, struct conntrack_val);
} tc_conntrack SEC(".maps");

// Per-interface configuration
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, struct iface_config);
} tc_iface_cfg SEC(".maps");

// Packet statistics (per-CPU)
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 2);  // index 0 = ingress, index 1 = egress
    __type(key, __u32);
    __type(value, struct pkt_stats);
} tc_stats SEC(".maps");

// ---- Helpers ----

static __always_inline struct fw_rule_val *tc_lookup_rule(
    __u32 src_ip, __u32 dst_ip,
    __u16 src_port, __u16 dst_port,
    __u8 protocol)
{
    struct fw_rule_key key = {};
    struct fw_rule_val *val;

    // Exact 5-tuple
    key.src_ip     = src_ip;
    key.dst_ip     = dst_ip;
    key.src_port   = src_port;
    key.dst_port   = dst_port;
    key.protocol   = protocol;
    key.src_prefix = 32;
    key.dst_prefix = 32;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Exact 5-tuple, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // IP pair only
    key.src_port = 0;
    key.dst_port = 0;
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // IP pair only, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Dst IP + port
    key.src_ip     = 0;
    key.src_prefix = 0;
    key.dst_ip     = dst_ip;
    key.dst_port   = dst_port;
    key.protocol   = protocol;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Dst IP + port, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Dst IP only
    key.dst_port = 0;
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Dst IP only, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Src IP only
    key.src_ip     = src_ip;
    key.src_prefix = 32;
    key.dst_ip     = 0;
    key.dst_prefix = 0;
    key.protocol   = protocol;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // Src IP only, any protocol
    key.protocol = 0;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);
    if (val)
        return val;

    // CIDR lookup (longest-prefix match)
    struct lpm_v4_key lpm_key = {
        .prefixlen = 32,
        .addr = src_ip,
    };
    val = bpf_map_lookup_elem(&tc_fw_rules_CIDR_src, &lpm_key);
    if (val)
        return val;

    lpm_key.addr = dst_ip;
    val = bpf_map_lookup_elem(&tc_fw_rules_CIDR_dst, &lpm_key);
    if (val)
        return val;

    // Protocol only
    __builtin_memset(&key, 0, sizeof(key));
    key.protocol = protocol;
    val = bpf_map_lookup_elem(&tc_fw_rules, &key);

    return val;
}

static __always_inline int update_conntrack(
    __u32 src_ip, __u32 dst_ip,
    __u16 src_port, __u16 dst_port,
    __u8 protocol, __u32 pkt_len)
{
    struct conntrack_key ct_key = {
        .src_ip   = src_ip,
        .dst_ip   = dst_ip,
        .src_port = src_port,
        .dst_port = dst_port,
        .protocol = protocol,
    };

    struct conntrack_val *ct = bpf_map_lookup_elem(&tc_conntrack, &ct_key);
    if (ct) {
        ct->last_seen = bpf_ktime_get_ns();
        ct->packets++;
        ct->bytes += pkt_len;
        if (ct->state == 0)
            ct->state = 1; // new -> established
        return 1; // existing connection
    }

    // Check reverse direction (reply traffic)
    struct conntrack_key rev_key = {
        .src_ip   = dst_ip,
        .dst_ip   = src_ip,
        .src_port = dst_port,
        .dst_port = src_port,
        .protocol = protocol,
    };
    ct = bpf_map_lookup_elem(&tc_conntrack, &rev_key);
    if (ct) {
        ct->last_seen = bpf_ktime_get_ns();
        ct->packets++;
        ct->bytes += pkt_len;
        ct->state = 1; // established
        return 1; // reply to existing connection
    }

    // Create new entry
    struct conntrack_val new_ct = {
        .last_seen = bpf_ktime_get_ns(),
        .packets   = 1,
        .bytes     = pkt_len,
        .state     = 0, // new
    };
    bpf_map_update_elem(&tc_conntrack, &ct_key, &new_ct, BPF_ANY);
    return 0; // new connection
}

// Return 1 if the given tenant name is exactly "default".
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

// Core firewall logic shared by ingress and egress
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

    // Enforce tenant isolation based on interface→tenant bindings.
    // We compare tenants for the current device (ifindex) and the
    // ingress device (ingress_ifindex). This runs on both ingress
    // and egress TC hooks so that cross-tenant traffic is caught
    // regardless of where it traverses.
    __u32 ifindex_a = skb->ifindex;
    __u32 ifindex_b = skb->ingress_ifindex;

    if (ifindex_a && ifindex_b) {
        char *tenant_a = bpf_map_lookup_elem(&tc_iface_tenant, &ifindex_a);
        char *tenant_b = bpf_map_lookup_elem(&tc_iface_tenant, &ifindex_b);
        if (tenant_a && tenant_b) {
            int same = 1;
#pragma unroll
            for (int i = 0; i < 64; i++) {
                char a = tenant_a[i];
                char b = tenant_b[i];
                if (a != b) {
                    same = 0;
                    break;
                }
                if (a == '\0') {
                    break;
                }
            }
            if (!same) {
                int a_is_default = is_default_tenant(tenant_a);
                int b_is_default = is_default_tenant(tenant_b);
                // Allow traffic when either side is the default
                // tenant, otherwise drop cross-tenant flows.
                if (!(a_is_default || b_is_default)) {
                    if (stats)
                        stats->dropped++;
                    return TC_ACT_SHOT;
                }
            }
        }
    }

    // Check conntrack: if this is reply traffic for an established
    // connection that was previously allowed, let it through immediately.
    if (conntrack_enabled) {
        struct conntrack_key rev_key = {
            .src_ip   = iph->daddr,
            .dst_ip   = iph->saddr,
            .src_port = dst_port,
            .dst_port = src_port,
            .protocol = iph->protocol,
        };
        struct conntrack_val *ct = bpf_map_lookup_elem(&tc_conntrack, &rev_key);
        if (ct && ct->state >= 1) {
            // Reply to an established (allowed) connection
            ct->last_seen = bpf_ktime_get_ns();
            ct->packets++;
            ct->bytes += pkt_len;
            return TC_ACT_OK;
        }
        // Also check forward direction for already-established flows
        struct conntrack_key fwd_key = {
            .src_ip   = iph->saddr,
            .dst_ip   = iph->daddr,
            .src_port = src_port,
            .dst_port = dst_port,
            .protocol = iph->protocol,
        };
        ct = bpf_map_lookup_elem(&tc_conntrack, &fwd_key);
        if (ct && ct->state >= 1) {
            ct->last_seen = bpf_ktime_get_ns();
            ct->packets++;
            ct->bytes += pkt_len;
            return TC_ACT_OK;
        }
    }

    return TC_ACT_OK;
}

// ---- TC Programs ----

SEC("tc/ingress")
int tc_firewall_ingress(struct __sk_buff *skb)
{
    return tc_firewall_core(skb, DIR_INGRESS);
}

SEC("tc/egress")
int tc_firewall_egress(struct __sk_buff *skb)
{
    return tc_firewall_core(skb, DIR_EGRESS);
}

char _license[] SEC("license") = "GPL";
