#include <linux/bpf.h>
#include <linux/in.h>
#include <bpf/bpf_helpers.h>

#define SERVICE_FRONTEND_MAX_ENTRIES 16384
#define SERVICE_BACKEND_MAX_ENTRIES 131072
#define SERVICE_REVNAT_MAX_ENTRIES 262144
#define SERVICE_STATS_MAX_ENTRIES 6

#define SOCK_STREAM 1
#define SOCK_DGRAM 2

enum service_stat_key {
    SERVICE_STAT_FRONTEND_HITS = 0,
    SERVICE_STAT_FRONTEND_MISSES = 1,
    SERVICE_STAT_NO_BACKENDS = 2,
    SERVICE_STAT_TRANSLATIONS = 3,
    SERVICE_STAT_REVNAT_HITS = 4,
    SERVICE_STAT_REVNAT_MISSES = 5,
};

struct service_frontend_key {
    __u32 address;
    __u16 port;
    __u8 protocol;
    __u8 pad;
};

struct service_frontend_value {
    __u32 backend_count;
    __u32 flags;
};

struct service_backend_key {
    struct service_frontend_key frontend;
    __u32 slot;
};

struct service_backend_value {
    __u32 address;
    __u16 port;
    __u8 flags;
    __u8 pad;
    char tenant[64];
};

struct service_revnat_key {
    __u64 socket_cookie;
    __u32 backend_address;
    __u16 backend_port;
    __u16 pad;
};

struct service_revnat_value {
    __u32 frontend_address;
    __u16 frontend_port;
    __u8 protocol;
    __u8 pad;
};

_Static_assert(sizeof(struct service_frontend_key) == 8, "frontend key ABI");
_Static_assert(sizeof(struct service_frontend_value) == 8, "frontend value ABI");
_Static_assert(sizeof(struct service_backend_key) == 12, "backend key ABI");
_Static_assert(sizeof(struct service_backend_value) == 72, "backend value ABI");
_Static_assert(sizeof(struct service_revnat_key) == 16, "socket revnat key ABI");
_Static_assert(sizeof(struct service_revnat_value) == 8, "socket revnat value ABI");

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
    __uint(max_entries, SERVICE_REVNAT_MAX_ENTRIES);
    __type(key, struct service_revnat_key);
    __type(value, struct service_revnat_value);
} svc_sock_revnat SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, SERVICE_STATS_MAX_ENTRIES);
    __type(key, __u32);
    __type(value, __u64);
} svc_sock_stats SEC(".maps");

static __always_inline void record_stat(__u32 key)
{
    __u64 *value = bpf_map_lookup_elem(&svc_sock_stats, &key);
    if (value)
        (*value)++;
}

static __always_inline __u8 socket_protocol(const struct bpf_sock_addr *ctx)
{
    if (ctx->type == SOCK_STREAM)
        return IPPROTO_TCP;
    if (ctx->type == SOCK_DGRAM)
        return IPPROTO_UDP;
    return 0;
}

static __always_inline struct service_backend_value *select_backend(
    const struct service_frontend_key *frontend,
    const struct service_frontend_value *service)
{
    if (!service || service->backend_count == 0)
        return NULL;

    struct service_backend_key backend_key = {};
    backend_key.frontend = *frontend;
    backend_key.slot = bpf_get_prandom_u32() % service->backend_count;

    return bpf_map_lookup_elem(&svc_backend, &backend_key);
}

static __always_inline int remember_translation(
    struct bpf_sock_addr *ctx,
    const struct service_frontend_key *frontend,
    __u32 backend_address,
    __u16 backend_port)
{
    __u64 cookie = bpf_get_socket_cookie(ctx);
    if (cookie == 0)
        return 0;

    struct service_revnat_key key = {
        .socket_cookie = cookie,
        .backend_address = backend_address,
        .backend_port = backend_port,
    };

    struct service_revnat_value value = {
        .frontend_address = frontend->address,
        .frontend_port = frontend->port,
        .protocol = frontend->protocol,
    };

    return bpf_map_update_elem(
        &svc_sock_revnat,
        &key,
        &value,
        BPF_ANY) == 0;
}

static __always_inline int translate_service(
    struct bpf_sock_addr *ctx,
    __u8 protocol)
{
    struct service_frontend_key frontend = {
        .address = ctx->user_ip4,
        .port = (__u16)ctx->user_port,
        .protocol = protocol,
    };

    struct service_frontend_value *service = bpf_map_lookup_elem(
        &svc_frontend,
        &frontend);
    if (!service) {
        record_stat(SERVICE_STAT_FRONTEND_MISSES);
        return 1;
    }

    record_stat(SERVICE_STAT_FRONTEND_HITS);

    struct service_backend_value *backend = select_backend(&frontend, service);
    if (!backend) {
        record_stat(SERVICE_STAT_NO_BACKENDS);
        return 1;
    }

    // Read only the fields needed by the socket path. The backend map also
    // contains tenant metadata for TC policy, but socket load balancing does
    // not need to copy or compare the tenant string.
    __u32 backend_address = backend->address;
    __u16 backend_port = backend->port;

    // Keep kube-proxy as the fallback when reverse-translation state cannot
    // be stored. Do not rewrite only one side of the socket interaction.
    if (!remember_translation(
            ctx,
            &frontend,
            backend_address,
            backend_port))
        return 1;

    ctx->user_ip4 = backend_address;
    ctx->user_port = backend_port;
    record_stat(SERVICE_STAT_TRANSLATIONS);
    return 1;
}

static __always_inline int reverse_translate(
    struct bpf_sock_addr *ctx,
    __u8 protocol)
{
    __u64 cookie = bpf_get_socket_cookie(ctx);
    if (cookie == 0) {
        record_stat(SERVICE_STAT_REVNAT_MISSES);
        return 1;
    }

    struct service_revnat_key key = {
        .socket_cookie = cookie,
        .backend_address = ctx->user_ip4,
        .backend_port = (__u16)ctx->user_port,
    };

    struct service_revnat_value *value = bpf_map_lookup_elem(
        &svc_sock_revnat,
        &key);
    if (!value || value->protocol != protocol) {
        record_stat(SERVICE_STAT_REVNAT_MISSES);
        return 1;
    }

    ctx->user_ip4 = value->frontend_address;
    ctx->user_port = value->frontend_port;
    record_stat(SERVICE_STAT_REVNAT_HITS);
    return 1;
}

SEC("cgroup/connect4")
int setera_service_connect4(struct bpf_sock_addr *ctx)
{
    __u8 protocol = socket_protocol(ctx);
    if (protocol == 0)
        return 1;

    return translate_service(ctx, protocol);
}

SEC("cgroup/sendmsg4")
int setera_service_sendmsg4(struct bpf_sock_addr *ctx)
{
    if (ctx->type != SOCK_DGRAM)
        return 1;

    return translate_service(ctx, IPPROTO_UDP);
}

SEC("cgroup/recvmsg4")
int setera_service_recvmsg4(struct bpf_sock_addr *ctx)
{
    if (ctx->type != SOCK_DGRAM)
        return 1;

    return reverse_translate(ctx, IPPROTO_UDP);
}

SEC("cgroup/getpeername4")
int setera_service_getpeername4(struct bpf_sock_addr *ctx)
{
    __u8 protocol = socket_protocol(ctx);
    if (protocol == 0)
        return 1;

    return reverse_translate(ctx, protocol);
}

char _license[] SEC("license") = "GPL";
