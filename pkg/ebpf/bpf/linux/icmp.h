#ifndef _LINUX_ICMP_H
#define _LINUX_ICMP_H

#include "types.h"

/* Minimal ICMP header used by BPF programs. */

struct icmphdr {
    __u8   type;
    __u8   code;
    __sum16 checksum;
};

#endif /* _LINUX_ICMP_H */
