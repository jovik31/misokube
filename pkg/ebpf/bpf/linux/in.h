#ifndef _LINUX_IN_H
#define _LINUX_IN_H

#include "types.h"

/* Protocol numbers (normally from linux/in.h). */
#define IPPROTO_IP   0
#define IPPROTO_ICMP 1
#define IPPROTO_TCP  6
#define IPPROTO_UDP  17

struct in_addr {
    __be32 s_addr;
};

#endif /* _LINUX_IN_H */
