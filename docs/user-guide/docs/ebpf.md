# eBPF connection owners

On Linux, the agent runs without privilege and cannot see which process owns
a socket of another user. The dependency map then leaves those sockets out.
The optional `senhub-ebpf` helper fills that gap: it watches TCP connections
in the kernel and tells the agent which process opened them, and how many
segments each connection retransmitted. The agent itself keeps its profile
(a service user, no capability, `NoNewPrivileges`).

!!! info "Optional, off until you start it"
    - The `.deb` and `.rpm` packages install the helper and its unit, and
      leave the unit **disabled**. Nothing runs and nothing changes until you
      enable it.
    - It needs Linux **5.8 or newer** with BTF (`/sys/kernel/btf/vmlinux`).
      RHEL, AlmaLinux and Rocky Linux 8 (kernel 4.18) are not supported: the
      helper would need `CAP_SYS_ADMIN` there, which it never takes.
    - Windows and macOS are not supported.
    - It adds no key, metric or entity of its own: the dependency edges and
      `senhub.connection.*` series are the ones described in
      [Topology](topology.md), with the same keys.

## Turning it on

```bash
sudo systemctl enable --now senhub-ebpf
senhub-agent doctor        # Host section: "eBPF helper running"
```

The agent finds the helper by its socket, `/run/senhub-ebpf/ebpf.sock`
(group `senhub`, mode `0660`), at its next entity cycle. No restart. Without
the socket, the agent behaves exactly as it does without the helper.

To stop using it, `sudo systemctl disable --now senhub-ebpf`.

## What it changes

The agent attributes a socket to a process by reading the kernel's socket
table, then the owner of each socket through `/proc`. For a socket owned by
another user it falls back to the user id, which names the process only when
all the processes of that user share one executable name. With the helper
running, a socket the agent still cannot attribute is looked up by its
addresses and ports in the helper's table, and the process that connected it
is named by the same rule as every other attribution: the executable name the
operating system reports, `<name>@<host.id>` as service instance.

- A process that has already exited is not named: the edge is not emitted.
- A connection opened before the helper started is not known to it. It is
  attributed again once the connection is renewed.
- Retransmitted segments counted by the kernel for an attributed socket feed
  `senhub.connection.retransmits` of its edge. The larger of the kernel's
  per-socket counter and the helper's count is used, never their sum.

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `entities.ebpf.enabled` | `true` | Lets the dependency scan consult the helper when its socket exists. `false` ignores the helper even if it runs |

The helper itself takes command-line flags in its unit, none required:

| Flag | Default | Meaning |
|---|---|---|
| `-socket` | `/run/senhub-ebpf/ebpf.sock` | Socket the agent connects to |
| `-max-conns` | `16384` | Hard cap on tracked connections; the least recently active are dropped first |
| `-closed-ttl` | `10m` | How long a closed connection stays available to the agent |

## What runs, and what it may do

The unit `senhub-ebpf.service` runs as its own user, `senhub-ebpf`, in the
`senhub` group, with exactly `CAP_BPF` and `CAP_PERFMON` (ambient and bounding
set) and no other capability. It is sandboxed (`ProtectSystem=strict`,
`ProtectHome`, `PrivateTmp`, `NoNewPrivileges`, only `AF_UNIX` sockets,
a system-call filter) and limited to 50 MB of memory.

It loads two kernel programs, on the tracepoints `inet_sock_set_state` and
`tcp_retransmit_skb`. They read the addresses, ports, TCP state and, at
connection time, the process id and command name. They read no packet and no
payload. The programs are compiled in advance and shipped in the binary: no
compiler or kernel headers are needed on the host.

When it cannot work, it says why in one line of the journal and stops, without
restart: kernel older than 5.8 or no BTF (exit code 3), `CAP_BPF` or
`CAP_PERFMON` missing (exit code 4).

```bash
journalctl -u senhub-ebpf
```

## `senhub-agent doctor`

The Host section reports the helper:

| State | Meaning |
|---|---|
| not installed | No helper binary or unit on this host. Informational |
| installed, not running | Start it with `systemctl enable --now senhub-ebpf`. Informational |
| unsupported | The kernel is older than 5.8. Informational |
| running | Version, kernel, BTF, connections tracked and events the kernel could not queue |
| not running (warning) | The socket exists but the helper does not answer; read its journal |
