# icmp_check — multi-target ping

Free tier. Pings a list of targets every cycle and reports reachability,
packet loss and round-trip-time statistics per target. This is the
agent's equivalent of the classic PRTG ping sensor, multi-target in a
single probe instance.

## Quick start

```yaml
# probes.d/20-ping.yaml
- name: ping-core
  type: icmp_check
  params:
    targets: ["10.0.0.1", "core-switch.lan", "8.8.8.8"]
    interval: 60
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `targets` | required | List of hostnames or IPs to ping |
| `count` | 4 | Echo requests per target per cycle |
| `timeout` | 5 | Per-target budget in seconds for the whole round |
| `interval` | 60 | Seconds between collection cycles |
| `packet_size` | 56 | ICMP payload size in bytes |
| `privileged` | OS-dependent | Raw ICMP sockets (`true`) vs ICMP datagram sockets (`false`). Defaults to `true` on Windows and on Linux when running as root, `false` elsewhere |

Targets are pinged in parallel (bounded), so a large list does not
stretch the cycle by the sum of timeouts.

## Metrics

One series per metric per target (the `target` tag discriminates; the
resolved `ip` rides along).

| Metric | Unit | Description |
|---|---|---|
| `senhub.icmp.up` | bool | 1 when at least one reply came back this cycle |
| `senhub.icmp.packet_loss` | % | Lost echo requests over the cycle |
| `senhub.icmp.packets.sent` / `.received` | count | Requests/replies this cycle |
| `senhub.icmp.rtt.min` / `.avg` / `.max` / `.stddev` | ms | Round-trip statistics (emitted only when at least one reply arrived) |

An unreachable target is a measurement (`up = 0`, loss 100%), not a
probe failure: the probe stays healthy and keeps reporting.

## Privileges

- **Linux**: running as root (the current agent requirement), the
  probe defaults to privileged raw sockets — unprivileged ICMP
  datagram sockets are gated by `net.ipv4.ping_group_range`, which
  stock Ubuntu/Debian servers ship disabled, and root does not bypass
  that sysctl. As a non-root process the default is unprivileged: if
  pings fail with a permission error, widen the range to include the
  agent's group or set `privileged: true` (requires `CAP_NET_RAW`).
  The permission error now carries this hint in the log.
- **Windows**: raw sockets only — the probe defaults to
  `privileged: true` and the agent service runs elevated.
- **macOS**: unprivileged mode works out of the box.
