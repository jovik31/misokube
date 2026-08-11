#ifndef _LINUX_IP_H
#define _LINUX_IP_H

#include "types.h"

/* Minimal IPv4 header definition for BPF programs. */

struct iphdr {
#if __BYTE_ORDER__ == __ORDER_LITTLE_ENDIAN__
    __u8 ihl:4,
         version:4;
#elif __BYTE_ORDER__ == __ORDER_BIG_ENDIAN__
    __u8 version:4,
         ihl:4;
#else
# error "Unknown endianness"
#endif
    __u8  tos;
    __be16 tot_len;
    __be16 id;
    __be16 frag_off;
    __u8  ttl;
    __u8  protocol;
    __sum16 check;
    __be32 saddr;
    __be32 daddr;
};

#endif /* _LINUX_IP_H */
