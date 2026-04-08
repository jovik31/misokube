#ifndef _LINUX_TYPES_H
#define _LINUX_TYPES_H

/* Minimal Linux types header for BPF builds.
 * This relies on our local asm/types.h stub for integer typedefs.
 */

/* Prefer search-path include so this works regardless of
 * where linux/types.h is found from (system vs local -I path).
 */
#include "asm/types.h"

/* Big- and little-endian integer typedefs expected by linux/bpf.h. */
typedef __u16 __be16;
typedef __u32 __be32;
typedef __u64 __be64;

typedef __u16 __le16;
typedef __u32 __le32;
typedef __u64 __le64;

/* Checksum type used by linux/ip.h, tcp.h, udp.h, icmp.h. */
typedef __u16 __sum16;

/* Checksum word type used by some BPF helpers (bpf_csum_*). */
typedef __u32 __wsum;

#endif /* _LINUX_TYPES_H */
