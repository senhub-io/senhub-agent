// SPDX-License-Identifier: GPL-2.0
//
// TCP connection and retransmission events for senhub-ebpf.
//
// Two BTF-enabled raw tracepoints (kernel 5.5+, loaded with CAP_BPF and
// CAP_PERFMON from 5.8) push fixed-size records to one ring buffer. A record
// that does not fit is counted in the dropped array, never blocked on.
#include "vmlinux.h"
#include <bpf_helpers.h>
#include <bpf_core_read.h>

#define IPPROTO_TCP 6
#define TCP_SYN_SENT 2

#define KIND_STATE 1
#define KIND_RETRANS 2

struct conn_event {
	__u64 skaddr;
	__u32 pid;
	__u32 tgid;
	__u16 sport;
	__u16 dport;
	__u16 family;
	__u8 kind;
	__u8 oldstate;
	__u8 newstate;
	__u8 pad[7];
	__u8 saddr[16];
	__u8 daddr[16];
	char comm[16];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);
} events SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__type(key, __u32);
	__type(value, __u64);
	__uint(max_entries, 1);
} dropped SEC(".maps");

static __always_inline void count_drop(void)
{
	__u32 zero = 0;
	__u64 *v = bpf_map_lookup_elem(&dropped, &zero);

	if (v)
		__sync_fetch_and_add(v, 1);
}

static __always_inline int emit(const struct sock *sk, __u8 kind, __u8 oldstate,
				__u8 newstate, int with_owner)
{
	struct conn_event *e;
	__u16 family;

	if (BPF_CORE_READ(sk, sk_protocol) != IPPROTO_TCP)
		return 0;
	family = BPF_CORE_READ(sk, __sk_common.skc_family);
	if (family != 2 && family != 10)
		return 0;

	e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) {
		count_drop();
		return 0;
	}
	__builtin_memset(e, 0, sizeof(*e));
	e->skaddr = (__u64)sk;
	e->kind = kind;
	e->oldstate = oldstate;
	e->newstate = newstate;
	e->family = family;
	e->sport = BPF_CORE_READ(sk, __sk_common.skc_num);
	e->dport = __builtin_bswap16(BPF_CORE_READ(sk, __sk_common.skc_dport));
	if (family == 2) {
		__be32 s = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
		__be32 d = BPF_CORE_READ(sk, __sk_common.skc_daddr);

		__builtin_memcpy(e->saddr, &s, 4);
		__builtin_memcpy(e->daddr, &d, 4);
	} else {
		bpf_core_read(e->saddr, sizeof(e->saddr), &sk->__sk_common.skc_v6_rcv_saddr);
		bpf_core_read(e->daddr, sizeof(e->daddr), &sk->__sk_common.skc_v6_daddr);
	}
	if (with_owner) {
		__u64 pt = bpf_get_current_pid_tgid();

		e->pid = (__u32)pt;
		e->tgid = pt >> 32;
		bpf_get_current_comm(&e->comm, sizeof(e->comm));
	}
	bpf_ringbuf_submit(e, 0);
	return 0;
}

/* The connecting thread is the one running when the socket enters SYN_SENT;
 * every later transition runs in softirq or an unrelated context, so the owner
 * is recorded only there. */
SEC("tp_btf/inet_sock_set_state")
int handle_set_state(__u64 *ctx)
{
	const struct sock *sk = (const struct sock *)ctx[0];
	int oldstate = (int)ctx[1];
	int newstate = (int)ctx[2];

	return emit(sk, KIND_STATE, oldstate, newstate, newstate == TCP_SYN_SENT);
}

SEC("tp_btf/tcp_retransmit_skb")
int handle_retransmit(__u64 *ctx)
{
	const struct sock *sk = (const struct sock *)ctx[0];

	return emit(sk, KIND_RETRANS, 0, 0, 0);
}

char LICENSE[] SEC("license") = "GPL";
