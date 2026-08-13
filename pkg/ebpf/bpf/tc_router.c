#include "common.h"
#include "service_types.h"

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
    __type(key, __u32);
    __type(value, struct pkt_stats);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_stats SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 256);
    __type(key, __u32);
    __type(value, struct veth_tenant);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_podIDs SEC(".maps");

// Keep this map in the ELF for loader compatibility. The kernel routing table
// owns Pod forwarding in this phase.
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32);
    __uint(pinning, LIBBPF_PIN_BY_NAME);
} tc_vxlan_ifindex SEC(".maps");

// These Service maps are node-wide and shared with the Service watcher and
// socket load balancer. The loader injects the pinned kernel maps when each Pod
// TC program is loaded.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, SERVICE_FRONTEND_MAX_ENTRIES);
    __type(key, struct service_frontend_key);
    __type(value, struct service_frontend_value);
} svc_frontend SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, SERVICE_BACKEND_MAX_ENTRIES);
    __type(key, struct service_backend_key);
    __type(value, struct service_backend_value);
} svc_backend SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, SERVICE_PACKET_FLOW_MAX_ENTRIES);
    __type(key, struct service_packet_flow_key);
    __type(value, struct service_packet_flow_value);
} svc_pkt_flow SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, SERVICE_PACKET_REVNAT_MAX_ENTRIES);
    __type(key, struct service_packet_revnat_key);
    __type(value, struct service_packet_revnat_value);
} svc_pkt_revnat SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, SERVICE_PACKET_STATS_MAX_ENTRIES);
    __type(key, __u32);
    __type(value, __u64);
} svc_pkt_stats SEC(".maps");

enum service_packet_stat_key {
    SERVICE_PACKET_STAT_FRONTEND_HITS = 0,
    SERVICE_PACKET_STAT_FLOW_HITS = 1,
    SERVICE_PACKET_STAT_FLOW_MISSES = 2,
    SERVICE_PACKET_STAT_TRANSLATIONS = 3,
    SERVICE_PACKET_STAT_REVNAT_HITS = 4,
    SERVICE_PACKET_STAT_POLICY_DROPS = 5,
    SERVICE_PACKET_STAT_FALLBACKS = 6,
};

struct service_l4 {
    __u16 source;
    __u16 destination;
    __u32 offset;
    __u32 checksum_offset;
};

static __always_inline int is_default_tenant(const char *name)
{
    if (!name)
        return 0;
    if (name[0] != 'd')
        return 0;
    if (name[1] != 'e')
        return 0;
    if (name[2] != 'f')
        return 0;
    if (name[3] != 'a')
        return 0;
    if (name[4] != 'u')
        return 0;
    if (name[5] != 'l')
        return 0;
    if (name[6] != 't')
        return 0;
    if (name[7] != '\0')
        return 0;

    return 1;
}

static __always_inline int tenant_matches(const char *tenant)
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

    if ((void *)iph + ((__u32)iph->ihl * 4) > data_end)
        return NULL;

    return iph;
}

static __always_inline int parse_service_l4(
    struct __sk_buff *skb,
    struct iphdr *iph,
    struct service_l4 *l4)
{
    if (!iph || !l4)
        return -1;

    // Do not perform packet Service NAT on fragmented IPv4 traffic in this
    // phase. kube-proxy remains the fallback for that path.
    if (iph->frag_off & bpf_htons(0x3fff))
        return -1;

    __u32 ip_header_len = (__u32)iph->ihl * 4;
    __u32 l4_offset = sizeof(struct ethhdr) + ip_header_len;
    void *data_end = (void *)(long)skb->data_end;
    void *l4_ptr = (void *)iph + ip_header_len;

    if (iph->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp = l4_ptr;
        if ((void *)(tcp + 1) > data_end)
            return -1;

        if (skb->len < l4_offset + 18)
            return -1;

        l4->source = tcp->source;
        l4->destination = tcp->dest;
        l4->offset = l4_offset;
        l4->checksum_offset = l4_offset + 16;

        return 0;
    }

    if (iph->protocol == IPPROTO_UDP) {
        struct udphdr *udp = l4_ptr;
        if ((void *)(udp + 1) > data_end)
            return -1;

        if (skb->len < l4_offset + 8)
            return -1;

        l4->source = udp->source;
        l4->destination = udp->dest;
        l4->offset = l4_offset;
        l4->checksum_offset = l4_offset + 6;

        return 0;
    }

    return -1;
}

static __always_inline struct pkt_stats *update_stats(
    struct __sk_buff *skb,
    __u32 direction)
{
    struct pkt_stats *stats = bpf_map_lookup_elem(
        &tc_stats,
        &direction);
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

static __always_inline void record_packet_stat(__u32 key)
{
    __u64 *value = bpf_map_lookup_elem(
        &svc_pkt_stats,
        &key);
    if (value)
        (*value)++;
}

static __always_inline __u64 packet_flow_timeout(__u8 protocol)
{
    if (protocol == IPPROTO_TCP)
        return SERVICE_PACKET_TCP_IDLE_NS;

    return SERVICE_PACKET_UDP_IDLE_NS;
}

static __always_inline int flow_expired(
    __u64 last_seen_ns,
    __u8 protocol,
    __u64 now_ns)
{
    if (last_seen_ns == 0 || now_ns < last_seen_ns)
        return 1;

    return now_ns - last_seen_ns > packet_flow_timeout(protocol);
}

static __always_inline int backend_allowed(
    __u32 backend_address,
    __u8 backend_flags,
    struct pkt_stats *stats)
{
    // The Service control plane marks whether this endpoint is a managed Pod.
    // Keep that bit in the flow so a missing tc_podIDs entry cannot turn a
    // managed backend into an unrestricted external destination.
    if (!(backend_flags & SERVICE_BACKEND_FLAG_MANAGED_POD))
        return 1;

    struct veth_tenant *backend = bpf_map_lookup_elem(
        &tc_podIDs,
        &backend_address);
    if (!backend) {
        if (stats)
            stats->dropped++;

        record_packet_stat(
            SERVICE_PACKET_STAT_POLICY_DROPS);

        return 0;
    }

    if (my_is_default || is_default_tenant(backend->tenant))
        return 1;

    if (tenant_matches(backend->tenant))
        return 1;

    if (stats)
        stats->dropped++;

    record_packet_stat(
        SERVICE_PACKET_STAT_POLICY_DROPS);

    return 0;
}

static __always_inline int rewrite_destination(
    struct __sk_buff *skb,
    __u8 protocol,
    const struct service_l4 *l4,
    __u32 old_address,
    __u32 new_address,
    __u16 old_port,
    __u16 new_port)
{
    __u32 ip_checksum_offset =
        sizeof(struct ethhdr) +
        __builtin_offsetof(struct iphdr, check);

    __u32 destination_offset =
        sizeof(struct ethhdr) +
        __builtin_offsetof(struct iphdr, daddr);

    __u64 pseudo_flags =
        BPF_F_PSEUDO_HDR |
        BPF_F_MARK_MANGLED_0 |
        4;

    __u64 port_flags =
        BPF_F_MARK_MANGLED_0 |
        2;

    if (bpf_l3_csum_replace(
            skb,
            ip_checksum_offset,
            old_address,
            new_address,
            4) < 0)
        return -1;

    if (bpf_l4_csum_replace(
            skb,
            l4->checksum_offset,
            old_address,
            new_address,
            pseudo_flags) < 0)
        return -1;

    if (old_port != new_port &&
        bpf_l4_csum_replace(
            skb,
            l4->checksum_offset,
            old_port,
            new_port,
            port_flags) < 0)
        return -1;

    if (bpf_skb_store_bytes(
            skb,
            destination_offset,
            &new_address,
            sizeof(new_address),
            BPF_F_INVALIDATE_HASH) < 0)
        return -1;

    if (old_port != new_port &&
        bpf_skb_store_bytes(
            skb,
            l4->offset + 2,
            &new_port,
            sizeof(new_port),
            0) < 0)
        return -1;

    return 0;
}

static __always_inline int rewrite_source(
    struct __sk_buff *skb,
    __u8 protocol,
    const struct service_l4 *l4,
    __u32 old_address,
    __u32 new_address,
    __u16 old_port,
    __u16 new_port)
{
    __u32 ip_checksum_offset =
        sizeof(struct ethhdr) +
        __builtin_offsetof(struct iphdr, check);

    __u32 source_offset =
        sizeof(struct ethhdr) +
        __builtin_offsetof(struct iphdr, saddr);

    __u64 pseudo_flags =
        BPF_F_PSEUDO_HDR |
        BPF_F_MARK_MANGLED_0 |
        4;

    __u64 port_flags =
        BPF_F_MARK_MANGLED_0 |
        2;

    if (bpf_l3_csum_replace(
            skb,
            ip_checksum_offset,
            old_address,
            new_address,
            4) < 0)
        return -1;

    if (bpf_l4_csum_replace(
            skb,
            l4->checksum_offset,
            old_address,
            new_address,
            pseudo_flags) < 0)
        return -1;

    if (old_port != new_port &&
        bpf_l4_csum_replace(
            skb,
            l4->checksum_offset,
            old_port,
            new_port,
            port_flags) < 0)
        return -1;

    if (bpf_skb_store_bytes(
            skb,
            source_offset,
            &new_address,
            sizeof(new_address),
            BPF_F_INVALIDATE_HASH) < 0)
        return -1;

    if (old_port != new_port &&
        bpf_skb_store_bytes(
            skb,
            l4->offset,
            &new_port,
            sizeof(new_port),
            0) < 0)
        return -1;

    return 0;
}

static __always_inline struct service_backend_value *select_packet_backend(
    const struct service_frontend_key *frontend,
    const struct service_frontend_value *service)
{
    if (!service || service->backend_count == 0)
        return NULL;

    struct service_backend_key key = {};

    key.frontend = *frontend;
    key.slot =
        bpf_get_prandom_u32() %
        service->backend_count;

    return bpf_map_lookup_elem(
        &svc_backend,
        &key);
}

static __always_inline int get_or_create_packet_flow(
    const struct service_frontend_key *frontend,
    const struct service_frontend_value *service,
    __u32 client_address,
    __u16 client_port,
    __u64 now_ns,
    struct service_packet_flow_key *flow_key,
    __u32 *backend_address,
    __u16 *backend_port,
    __u8 *backend_flags)
{
    *flow_key = (struct service_packet_flow_key){
        .client_address = client_address,
        .frontend_address = frontend->address,
        .client_port = client_port,
        .frontend_port = frontend->port,
        .protocol = frontend->protocol,
    };

    struct service_packet_flow_value *existing =
        bpf_map_lookup_elem(
            &svc_pkt_flow,
            flow_key);

    if (existing &&
        !flow_expired(
            existing->last_seen_ns,
            frontend->protocol,
            now_ns)) {
        existing->last_seen_ns = now_ns;

        *backend_address =
            existing->backend_address;

        *backend_port =
            existing->backend_port;

        *backend_flags =
            existing->backend_flags;

        record_packet_stat(
            SERVICE_PACKET_STAT_FLOW_HITS);

        return 1;
    }

    if (existing)
        bpf_map_delete_elem(
            &svc_pkt_flow,
            flow_key);

    record_packet_stat(
        SERVICE_PACKET_STAT_FLOW_MISSES);

    struct service_backend_value *selected =
        select_packet_backend(
            frontend,
            service);

    if (!selected)
        return 0;

    __u32 selected_address =
        selected->address;

    __u16 selected_port =
        selected->port;

    __u8 selected_flags =
        selected->flags;

    // Keep only the fields needed after backend selection. Do not copy the
    // complete service_backend_value, which includes the 64-byte tenant name.
    struct service_packet_flow_value new_flow = {
        .backend_address = selected_address,
        .backend_port = selected_port,
        .backend_flags = selected_flags,
        .last_seen_ns = now_ns,
    };

    if (bpf_map_update_elem(
            &svc_pkt_flow,
            flow_key,
            &new_flow,
            BPF_NOEXIST) == 0) {
        *backend_address =
            selected_address;

        *backend_port =
            selected_port;

        *backend_flags =
            selected_flags;

        return 1;
    }

    // Another CPU may have installed the flow between the lookup and update.
    // Reuse that winner so all packets in the flow select the same backend.
    existing = bpf_map_lookup_elem(
        &svc_pkt_flow,
        flow_key);

    if (existing &&
        !flow_expired(
            existing->last_seen_ns,
            frontend->protocol,
            now_ns)) {
        existing->last_seen_ns = now_ns;

        *backend_address =
            existing->backend_address;

        *backend_port =
            existing->backend_port;

        *backend_flags =
            existing->backend_flags;

        return 1;
    }

    record_packet_stat(
        SERVICE_PACKET_STAT_FALLBACKS);

    return 0;
}

static __always_inline int ensure_packet_revnat(
    const struct service_packet_flow_key *flow_key,
    __u32 backend_address,
    __u16 backend_port,
    __u64 now_ns)
{
    struct service_packet_revnat_key key = {
        .backend_address = backend_address,
        .client_address = flow_key->client_address,
        .backend_port = backend_port,
        .client_port = flow_key->client_port,
        .protocol = flow_key->protocol,
    };

    struct service_packet_revnat_value *existing =
        bpf_map_lookup_elem(
            &svc_pkt_revnat,
            &key);

    if (existing &&
        flow_expired(
            existing->last_seen_ns,
            key.protocol,
            now_ns)) {
        bpf_map_delete_elem(
            &svc_pkt_revnat,
            &key);

        existing = NULL;
    }

    if (existing) {
        if (existing->frontend_address !=
                flow_key->frontend_address ||
            existing->frontend_port !=
                flow_key->frontend_port) {
            record_packet_stat(
                SERVICE_PACKET_STAT_FALLBACKS);

            return 0;
        }

        existing->last_seen_ns = now_ns;
        return 1;
    }

    struct service_packet_revnat_value value = {
        .frontend_address =
            flow_key->frontend_address,

        .frontend_port =
            flow_key->frontend_port,

        .last_seen_ns =
            now_ns,
    };

    if (bpf_map_update_elem(
            &svc_pkt_revnat,
            &key,
            &value,
            BPF_NOEXIST) == 0)
        return 1;

    // Handle a create race by accepting the entry only when it describes the
    // same Service frontend.
    existing = bpf_map_lookup_elem(
        &svc_pkt_revnat,
        &key);

    if (existing &&
        existing->frontend_address ==
            flow_key->frontend_address &&
        existing->frontend_port ==
            flow_key->frontend_port) {
        existing->last_seen_ns = now_ns;
        return 1;
    }

    record_packet_stat(
        SERVICE_PACKET_STAT_FALLBACKS);

    return 0;
}

static __always_inline int translate_packet_service(
    struct __sk_buff *skb,
    struct iphdr *iph,
    struct pkt_stats *stats)
{
    struct service_l4 l4 = {};

    if (parse_service_l4(
            skb,
            iph,
            &l4) < 0)
        return TC_ACT_OK;

    struct service_frontend_key frontend = {
        .address = iph->daddr,
        .port = l4.destination,
        .protocol = iph->protocol,
    };

    struct service_frontend_value *service =
        bpf_map_lookup_elem(
            &svc_frontend,
            &frontend);

    if (!service)
        return TC_ACT_OK;

    record_packet_stat(
        SERVICE_PACKET_STAT_FRONTEND_HITS);

    __u64 now_ns =
        bpf_ktime_get_ns();

    struct service_packet_flow_key flow_key = {};

    __u32 backend_address = 0;
    __u16 backend_port = 0;
    __u8 backend_flags = 0;

    if (!get_or_create_packet_flow(
            &frontend,
            service,
            iph->saddr,
            l4.source,
            now_ns,
            &flow_key,
            &backend_address,
            &backend_port,
            &backend_flags))
        return TC_ACT_OK;

    if (!backend_allowed(
            backend_address,
            backend_flags,
            stats)) {
        bpf_map_delete_elem(
            &svc_pkt_flow,
            &flow_key);

        return TC_ACT_SHOT;
    }

    if (!ensure_packet_revnat(
            &flow_key,
            backend_address,
            backend_port,
            now_ns)) {
        // Leave the packet unchanged so kube-proxy can handle this flow.
        bpf_map_delete_elem(
            &svc_pkt_flow,
            &flow_key);

        return TC_ACT_OK;
    }

    if (rewrite_destination(
            skb,
            frontend.protocol,
            &l4,
            frontend.address,
            backend_address,
            frontend.port,
            backend_port) < 0) {
        record_packet_stat(
            SERVICE_PACKET_STAT_FALLBACKS);

        return TC_ACT_SHOT;
    }

    record_packet_stat(
        SERVICE_PACKET_STAT_TRANSLATIONS);

    return TC_ACT_OK;
}

static __always_inline int reverse_packet_service(
    struct __sk_buff *skb,
    struct iphdr *iph)
{
    struct service_l4 l4 = {};

    if (parse_service_l4(
            skb,
            iph,
            &l4) < 0)
        return TC_ACT_OK;

    struct service_packet_revnat_key key = {
        .backend_address = iph->saddr,
        .client_address = iph->daddr,
        .backend_port = l4.source,
        .client_port = l4.destination,
        .protocol = iph->protocol,
    };

    struct service_packet_revnat_value *value =
        bpf_map_lookup_elem(
            &svc_pkt_revnat,
            &key);

    if (!value)
        return TC_ACT_OK;

    __u64 now_ns =
        bpf_ktime_get_ns();

    if (flow_expired(
            value->last_seen_ns,
            key.protocol,
            now_ns)) {
        bpf_map_delete_elem(
            &svc_pkt_revnat,
            &key);

        return TC_ACT_OK;
    }

    __u32 frontend_address =
        value->frontend_address;

    __u16 frontend_port =
        value->frontend_port;

    value->last_seen_ns =
        now_ns;

    // Refresh the forward flow when the return path is active. This keeps a
    // long-lived TCP connection pinned even when it is mostly receiving data.
    struct service_packet_flow_key flow_key = {
        .client_address = key.client_address,
        .frontend_address = frontend_address,
        .client_port = key.client_port,
        .frontend_port = frontend_port,
        .protocol = key.protocol,
    };

    struct service_packet_flow_value *flow =
        bpf_map_lookup_elem(
            &svc_pkt_flow,
            &flow_key);

    if (flow)
        flow->last_seen_ns = now_ns;

    __u32 old_address =
        key.backend_address;

    __u16 old_port =
        key.backend_port;

    if (rewrite_source(
            skb,
            key.protocol,
            &l4,
            old_address,
            frontend_address,
            old_port,
            frontend_port) < 0) {
        record_packet_stat(
            SERVICE_PACKET_STAT_FALLBACKS);

        return TC_ACT_SHOT;
    }

    record_packet_stat(
        SERVICE_PACKET_STAT_REVNAT_HITS);

    return TC_ACT_OK;
}

// Ingress runs on the host side of the Pod veth for traffic sent by the Pod.
// Direct managed-Pod traffic stays on the existing tenant-policy path. Only an
// unknown TCP/UDP destination is checked against the Service frontend map, so
// the normal direct-Pod and socket-LB paths do not pay Service flow/NAT costs.
static __always_inline int tc_ingress_core(
    struct __sk_buff *skb)
{
    struct iphdr *iph =
        parse_ipv4(skb);

    if (!iph)
        return TC_ACT_OK;

    __u32 dst_ip =
        iph->daddr;

    struct pkt_stats *stats =
        update_stats(
            skb,
            DIR_INGRESS);

    struct veth_tenant *dst_info =
        bpf_map_lookup_elem(
            &tc_podIDs,
            &dst_ip);

    if (dst_info) {
        if (my_is_default ||
            is_default_tenant(dst_info->tenant))
            return TC_ACT_OK;

        if (!tenant_matches(
                dst_info->tenant)) {
            if (stats)
                stats->dropped++;

            return TC_ACT_SHOT;
        }

        return TC_ACT_OK;
    }

    // The socket LB rewrites a Service to a backend before packets reach TC,
    // so socket-LB traffic takes the direct managed-Pod path above. A Service
    // VIP reaches this fallback only when socket-level translation did not run.
    return translate_packet_service(
        skb,
        iph,
        stats);
}

// Egress runs on the host side of the destination Pod veth. Tenant policy is
// evaluated against the real backend source before packet Service reverse NAT
// restores the Service source address for the client.
static __always_inline int tc_egress_core(
    struct __sk_buff *skb)
{
    struct iphdr *iph =
        parse_ipv4(skb);

    if (!iph)
        return TC_ACT_OK;

    __u32 src_ip =
        iph->saddr;

    struct pkt_stats *stats =
        update_stats(
            skb,
            DIR_EGRESS);

    struct veth_tenant *src_info =
        bpf_map_lookup_elem(
            &tc_podIDs,
            &src_ip);

    if (src_info) {
        if (!my_is_default &&
            !is_default_tenant(src_info->tenant) &&
            !tenant_matches(src_info->tenant)) {
            if (stats)
                stats->dropped++;

            return TC_ACT_SHOT;
        }
    }

    return reverse_packet_service(
        skb,
        iph);
}

SEC("tc/ingress")
int tc_firewall_ingress(
    struct __sk_buff *skb)
{
    return tc_ingress_core(skb);
}

SEC("tc/egress")
int tc_firewall_egress(
    struct __sk_buff *skb)
{
    return tc_egress_core(skb);
}

char _license[] SEC("license") = "GPL";
