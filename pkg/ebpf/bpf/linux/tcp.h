#ifndef _LINUX_TCP_H
#define _LINUX_TCP_H

#include "types.h"

/* Minimal TCP header used by BPF programs (only ports are accessed). */

struct tcphdr {
    __be16 source;
    __be16 dest;
};

#endif /* _LINUX_TCP_H */
