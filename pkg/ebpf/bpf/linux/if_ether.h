#ifndef _LINUX_IF_ETHER_H
#define _LINUX_IF_ETHER_H

#include "types.h"

/* Minimal Ethernet header and constants for BPF programs. */

#define ETH_ALEN 6

/* EtherType values we care about */
#define ETH_P_IP  0x0800 /* Internet Protocol packet */

struct ethhdr {
	unsigned char h_dest[ETH_ALEN];   /* destination eth addr */
	unsigned char h_source[ETH_ALEN]; /* source ether addr    */
	__be16        h_proto;            /* packet type ID field */
};

#endif /* _LINUX_IF_ETHER_H */

