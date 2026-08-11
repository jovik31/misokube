#ifndef _LINUX_IPV6_H
#define _LINUX_IPV6_H

#include "types.h"

/* Minimal IPv6 header placeholder; current programs do not parse IPv6. */

struct in6_addr {
    __u8 s6_addr[16];
};

struct ipv6hdr {
    __u8  priority:4,
          version:4;
    __u8  flow_lbl[3];
    __be16 payload_len;
    __u8  nexthdr;
    __u8  hop_limit;
    struct in6_addr saddr;
    struct in6_addr daddr;
};

#endif /* _LINUX_IPV6_H */
