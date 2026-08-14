#ifndef _ASM_TYPES_H
#define _ASM_TYPES_H

/* Minimal integer typedefs for BPF programs, independent of libc. */

typedef unsigned char      __u8;
typedef unsigned short     __u16;
typedef unsigned int       __u32;
typedef unsigned long long __u64;

typedef signed char        __s8;
typedef short              __s16;
typedef int                __s32;
typedef long long          __s64;

/* Aligned 64-bit type used by linux/bpf.h */
typedef __u64 __aligned_u64 __attribute__((aligned(8)));

#endif /* _ASM_TYPES_H */
