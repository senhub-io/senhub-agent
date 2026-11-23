/* Minimal CO-RE type definitions. Only the kernel structures and fields the
 * programs read are declared; libbpf relocates every access against the BTF of
 * the running kernel by name, so layouts here need not match any kernel. */
#ifndef __SENHUB_VMLINUX_H__
#define __SENHUB_VMLINUX_H__

typedef unsigned char __u8;
typedef unsigned short __u16;
typedef unsigned int __u32;
typedef unsigned long long __u64;
typedef signed char __s8;
typedef short __s16;
typedef int __s32;
typedef long long __s64;
typedef __u16 __be16;
typedef __u16 __sum16;
typedef __u32 __wsum;
typedef __u32 __be32;
typedef __u8 u8;
typedef __u16 u16;
typedef __u32 u32;
typedef __u64 u64;

#define BPF_MAP_TYPE_ARRAY 2
#define BPF_MAP_TYPE_RINGBUF 27

#pragma clang attribute push(__attribute__((preserve_access_index)), apply_to = record)

struct in6_addr {
	union {
		__u8 u6_addr8[16];
	} in6_u;
};

struct sock_common {
	__be32 skc_daddr;
	__be32 skc_rcv_saddr;
	__be16 skc_dport;
	__u16 skc_num;
	__u16 skc_family;
	struct in6_addr skc_v6_daddr;
	struct in6_addr skc_v6_rcv_saddr;
};

struct sock {
	struct sock_common __sk_common;
	__u16 sk_protocol;
};

#pragma clang attribute pop

#endif
