#ifndef _LINUX_UDP_H
#define _LINUX_UDP_H

#include "types.h"

/* Minimal UDP header used by BPF programs. */

struct udphdr {
    __be16 source;
    __be16 dest;
    __be16 len;
    __sum16 check;
};

#endif /* _LINUX_UDP_H */
