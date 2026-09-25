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

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `targets` | Yes | - | host:port pairs to dial. Example: `10.0.0.10:443, dc01.lan:389` |
| `timeout` | No | `5` | Connect budget in seconds per target |
| `interval` | No | `60` | Seconds between cycles |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.tcpdial.up` | bool | Connect completed within the timeout |
| `senhub.tcpdial.duration` | ms | Three-way-handshake time (emitted only when up) |

A refused or timed out target is a measurement (`up = 0`), never a
probe failure.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.tcpdial.up` | `tcpdial_up` | # | 1 when the TCP connect completed within the timeout |
| `senhub.tcpdial.duration` | `tcpdial_duration` | ms | Time for the TCP three-way handshake to complete |

<!-- schema:metrics:end -->
