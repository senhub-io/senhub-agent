<img src="../../assets/probe-logos/tcp-dial.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

# tcp_dial — TCP connect latency

Free tier. Measures the time for a raw TCP `connect()` to complete
against `host:port` targets. For a load-balancer VIP, a Citrix broker,
a domain controller or a fileserver, a measured dial is faster and more
dependable than an HTTP round trip.

## Quick start

```yaml
# probes.d/40-dial.yaml
- name: dial-core
  type: tcp_dial
  params:
    targets: ["10.0.0.10:443", "dc01.lan:389", "files.lan:445"]
    interval: 60
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:17381de966be9fd9a3970c024a535c969748e6dcef8957a8a090695c0fe7cb0d -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `targets` | Yes | - | host:port pairs to dial. Example: `10.0.0.10:443, dc01.lan:389` |
| `timeout` | No | `5` | Connect budget in seconds per target |
| `interval` | No | `60` | Seconds between cycles |
| `mode` | No | `auto` | auto: read the TCP table for a target on this host and dial a remote one; dial: always connect; local: always read the TCP table |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.tcpdial.up` | bool | Connect completed within the timeout |
| `senhub.tcpdial.duration` | s | Three-way-handshake time (emitted only when up) |

A refused or timed out target is a measurement (`up = 0`), never a
probe failure.

### Targets on the agent's own host

A connect-and-close has side effects on the service it checks: on one
host, a dial every minute against a local port produced about 1,440
"unexpected eof" lines a day in that service's log. When a target
resolves to a loopback address or to one of the host's own addresses,
`mode: auto` (the default) reads the TCP table instead of connecting,
and emits per port:

| Metric | Unit | Description |
|---|---|---|
| `senhub.tcp.listener.up` | bool | A socket is in LISTEN on the port; `process.executable.name` names its process when the agent can read it |
| `senhub.tcp.listener.connections` | {connection} | ESTABLISHED sockets on the port |
| `senhub.tcp.listener.peers` | {peer} | Distinct remote addresses among them |

`senhub.tcpdial.up`, `senhub.tcpdial.duration` and the check result are
not emitted for such a target, since nothing was dialed. A port in
LISTEN proves that a process holds it, not that the service answers: a
protocol check (`http_check` or the service's own probe) remains the
proof. `mode: dial` keeps connecting to every target, `mode: local`
reads the table for every target.

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
| `senhub.tcpdial.up` | `senhub.tcpdial.up` | TCP {target} Up | # | 1 when the TCP connect completed within the timeout |
| `senhub.tcpdial.duration` | `senhub.tcpdial.duration` | TCP {target} Connect Time | ms | Time for the TCP three-way handshake to complete |
| `senhub.probe.check.result` | `senhub.probe.check.result` | Check Result | # | Why the last check failed: 0 ok, 1 target down, 2 auth denied, 3 timeout, 4 TLS error, 5 config error, 6 protocol error, 7 permission denied |
| `senhub.probe.check.target.unresolved` | `senhub.probe.check.target.unresolved` | Check Target Unresolved | # | 1 when a local target is not listening and the check failed with connection refused |
| `senhub.tcp.listener.up` | `senhub.tcp.listener.up` | TCP {target} Listener Up | # | 1 when a socket is in LISTEN on the local port, read from the TCP table |
| `senhub.tcp.listener.connections` | `senhub.tcp.listener.connections` | TCP {target} Listener Connections | {connection} | ESTABLISHED sockets on the local port, read from the TCP table |
| `senhub.tcp.listener.peers` | `senhub.tcp.listener.peers` | TCP {target} Listener Peers | {peer} | Distinct remote IPs with an ESTABLISHED socket on the local port |

<!-- schema:metrics:end -->
