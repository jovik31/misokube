#ifndef __COMMON_H__
#define __COMMON_H__

// ============================================================================
// Shared types and constants for Setera eBPF firewall programs (TC + XDP)
// ============================================================================

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/ipv6.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/icmp.h>
#include <linux/in.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// Firewall actions
#define FW_ACTION_DROP   0
#define FW_ACTION_ALLOW  1
#define FW_ACTION_LOG    2  // Allow but log the packet

// Direction for TC programs
#define DIR_INGRESS 0
#define DIR_EGRESS  1

// Maximum number of firewall rules
#define MAX_RULES 1024

// Maximum number of connection tracking entries
#define MAX_CONNTRACK 65536

// Hash map that stores the tenant name and veth interface for each pod
struct veth_tenant {
    char tenant[64]; // Null-terminated tenant name (zero-padded)
    __u32 veth_ifindex; // This is the node side interface of the veth pair. If this pod is in a different node the index is -1
};

//Key for LPM trie map (for CIDR-based rules)
struct lpm_v4_key {
    __u32 prefixlen;   // must be first for LPM
    __u32 addr;        // IPv4 address (network bits significant)
};

// Firewall rule key: identifies a rule by source/destination network + protocol
struct fw_rule_key {
    __u32 src_ip;       // Source IP (0 = any)
    __u32 dst_ip;       // Destination IP (0 = any)
    __u16 src_port;     // Source port (0 = any)
    __u16 dst_port;     // Destination port (0 = any)
    __u8  protocol;     // IPPROTO_TCP, IPPROTO_UDP, IPPROTO_ICMP (0 = any)
    __u8  src_prefix;   // Source prefix length (for CIDR matching)
    __u8  dst_prefix;   // Destination prefix length (for CIDR matching)
    __u8  pad;
};

// Firewall rule value: action + counters
struct fw_rule_val {
    __u32 action;       // FW_ACTION_DROP, FW_ACTION_ALLOW, FW_ACTION_LOG
    __u64 packets;      // Packet counter
    __u64 bytes;        // Byte counter
};

// Connection tracking entry (5-tuple)
struct conntrack_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  pad[3];
};

struct conntrack_val {
    __u64 last_seen;    // Timestamp (ktime_ns)
    __u64 packets;
    __u64 bytes;
    __u8  state;        // 0 = new, 1 = established, 2 = closing
    __u8  pad[7];
};

// Per-interface firewall config
struct iface_config {
    __u32 default_action;   // Default action when no rule matches
    __u32 flags;            // Bitmask: bit 0 = conntrack enabled
};

// Packet stats (per-CPU counters for a given interface)
struct pkt_stats {
    __u64 rx_packets;
    __u64 rx_bytes;
    __u64 tx_packets;
    __u64 tx_bytes;
    __u64 dropped;
};

// Apply a prefix mask to an IP address
static __always_inline __u32 apply_prefix(__u32 ip, __u8 prefix) {
    if (prefix == 0) return 0;
    if (prefix >= 32) return ip;
    return ip & bpf_htonl(~((__u32)0) << (32 - prefix));
}

// Parse the L4 header and extract ports
static __always_inline int parse_l4(void *data, void *data_end,
                                     struct iphdr *iph,
                                     __u16 *src_port, __u16 *dst_port) {
    __u32 iph_len = iph->ihl * 4;
    void *l4 = (void *)iph + iph_len;

    switch (iph->protocol) {
    case IPPROTO_TCP: {
        struct tcphdr *tcp = l4;
        if ((void *)(tcp + 1) > data_end)
            return -1;
        *src_port = bpf_ntohs(tcp->source);
        *dst_port = bpf_ntohs(tcp->dest);
        return 0;
    }
    case IPPROTO_UDP: {
        struct udphdr *udp = l4;
        if ((void *)(udp + 1) > data_end)
            return -1;
        *src_port = bpf_ntohs(udp->source);
        *dst_port = bpf_ntohs(udp->dest);
        return 0;
    }
    case IPPROTO_ICMP: {
        struct icmphdr *icmp = l4;
        if ((void *)(icmp + 1) > data_end)
            return -1;
        *src_port = 0;
        *dst_port = 0;
        return 0;
    }
    default:
        *src_port = 0;
        *dst_port = 0;
        return 0;
    }
}

#endif /* __COMMON_H__ */
