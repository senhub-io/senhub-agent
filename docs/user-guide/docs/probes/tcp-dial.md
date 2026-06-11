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

| Parameter | Default | Description |
|---|---|---|
| `targets` | required | List of `host:port` |
| `timeout` | 5 | Connect budget in seconds |
| `interval` | 60 | Seconds between cycles |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.tcpdial.up` | bool | Connect completed within the timeout |
| `senhub.tcpdial.duration` | ms | Three-way-handshake time (emitted only when up) |

A refused or timed out target is a measurement (`up = 0`), never a
probe failure.
