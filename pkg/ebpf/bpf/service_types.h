#ifndef __SETERA_SERVICE_TYPES_H__
#define __SETERA_SERVICE_TYPES_H__

#include <linux/types.h>

#define SERVICE_FRONTEND_MAX_ENTRIES 16384
#define SERVICE_BACKEND_MAX_ENTRIES 131072
#define SERVICE_SOCKET_REVNAT_MAX_ENTRIES 262144
#define SERVICE_SOCKET_STATS_MAX_ENTRIES 6
#define SERVICE_PACKET_FLOW_MAX_ENTRIES 262144
#define SERVICE_PACKET_REVNAT_MAX_ENTRIES 262144
#define SERVICE_PACKET_STATS_MAX_ENTRIES 7

#define SERVICE_BACKEND_FLAG_MANAGED_POD (1U << 0)

#define SERVICE_PACKET_TCP_IDLE_NS (3600ULL * 1000000000ULL)
#define SERVICE_PACKET_UDP_IDLE_NS (300ULL * 1000000000ULL)

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

struct service_socket_revnat_key {
    __u64 socket_cookie;
    __u32 backend_address;
    __u16 backend_port;
    __u16 pad;
};

struct service_socket_revnat_value {
    __u32 frontend_address;
    __u16 frontend_port;
    __u8 protocol;
    __u8 pad;
};

struct service_packet_flow_key {
    __u32 client_address;
    __u32 frontend_address;
    __u16 client_port;
    __u16 frontend_port;
    __u8 protocol;
    __u8 pad[3];
};

struct service_packet_flow_value {
    __u32 backend_address;
    __u16 backend_port;
    __u8 backend_flags;
    __u8 pad;
    __u64 last_seen_ns;
};

struct service_packet_revnat_key {
    __u32 backend_address;
    __u32 client_address;
    __u16 backend_port;
    __u16 client_port;
    __u8 protocol;
    __u8 pad[3];
};

struct service_packet_revnat_value {
    __u32 frontend_address;
    __u16 frontend_port;
    __u16 pad;
    __u64 last_seen_ns;
};

_Static_assert(sizeof(struct service_frontend_key) == 8, "frontend key ABI");
_Static_assert(sizeof(struct service_frontend_value) == 8, "frontend value ABI");
_Static_assert(sizeof(struct service_backend_key) == 12, "backend key ABI");
_Static_assert(sizeof(struct service_backend_value) == 72, "backend value ABI");
_Static_assert(sizeof(struct service_socket_revnat_key) == 16, "socket revnat key ABI");
_Static_assert(sizeof(struct service_socket_revnat_value) == 8, "socket revnat value ABI");
_Static_assert(sizeof(struct service_packet_flow_key) == 16, "packet flow key ABI");
_Static_assert(sizeof(struct service_packet_flow_value) == 16, "packet flow value ABI");
_Static_assert(sizeof(struct service_packet_revnat_key) == 16, "packet revnat key ABI");
_Static_assert(sizeof(struct service_packet_revnat_value) == 16, "packet revnat value ABI");

#endif /* __SETERA_SERVICE_TYPES_H__ */
