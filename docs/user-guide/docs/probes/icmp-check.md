<img src="../../assets/probe-logos/icmp-check.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

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

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `targets` | Yes | - | Hostnames or addresses to ping. Example: `10.0.0.1, gw.example.com` |
| `count` | No | `4` | Echo requests per target per cycle |
| `timeout` | No | `5` | Budget in seconds for the whole round on one target |
| `interval` | No | `60` | Seconds between cycles |
| `packet_size` | No | `56` | ICMP payload size in bytes |
| `privileged` | No | - | Raw ICMP sockets (true) or datagram sockets (false); default true on Windows and as root on Linux |

<!-- schema:params:end -->

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

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.icmp.up` | `senhub.icmp.up` | Ping {target} Reachability | # | 1 when the target answered at least one echo request in the last cycle, 0 otherwise |
| `senhub.icmp.packet_loss` | `senhub.icmp.packet_loss` | Ping {target} Packet Loss | % | Percentage of echo requests without a reply in the last cycle |
| `senhub.icmp.packets.sent` | `senhub.icmp.packets.sent` | Ping {target} Packets Sent | # | Echo requests sent in the last cycle |
| `senhub.icmp.packets.received` | `senhub.icmp.packets.received` | Ping {target} Packets Received | # | Echo replies received in the last cycle |
| `senhub.icmp.rtt.min` | `senhub.icmp.rtt.min` | Ping {target} RTT Min | ms | Minimum round-trip time over the last cycle |
| `senhub.icmp.rtt.avg` | `senhub.icmp.rtt.avg` | Ping {target} RTT Avg | ms | Average round-trip time over the last cycle |
| `senhub.icmp.rtt.max` | `senhub.icmp.rtt.max` | Ping {target} RTT Max | ms | Maximum round-trip time over the last cycle |
| `senhub.icmp.rtt.stddev` | `senhub.icmp.rtt.stddev` | Ping {target} RTT StdDev | ms | Round-trip time standard deviation (jitter proxy) over the last cycle |

<!-- schema:metrics:end -->
