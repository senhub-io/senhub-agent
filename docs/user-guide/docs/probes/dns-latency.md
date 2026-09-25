<img src="../../assets/probe-logos/dns-latency.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

# dns_latency — DNS resolution latency

Free tier. Measures resolution latency for a set of names — through the
operating system's resolver by default, or against explicit DNS servers
to compare them. Slow DNS is a frequent cause of perceived slowness
(logon time, application launch, share access).

## Quick start

```yaml
# probes.d/50-dns.yaml
- name: dns-checks
  type: dns_latency
  params:
    names: ["intranet.corp.lan", "www.example.com"]
    resolvers: ["10.0.0.53", "1.1.1.1"]   # optional; omit for the system resolver
    interval: 60
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `names` | Yes | - | Names to resolve. Example: `intranet.corp.lan, www.example.com` |
| `resolvers` | No | - | DNS servers as ip or ip:port, each name measured against each; empty uses the system resolver. Example: `10.0.0.53, 1.1.1.1` |
| `timeout` | No | `5` | Budget in seconds per lookup |
| `interval` | No | `60` | Seconds between cycles |

<!-- schema:params:end -->

## Metrics

One series per (name, resolver) pair — `resolver` is `system` when no
explicit servers are configured.

| Metric | Unit | Description |
|---|---|---|
| `senhub.dns.up` | bool | Lookup answered within the timeout |
| `senhub.dns.lookup.duration` | ms | Resolution time (only when up) |
| `senhub.dns.answers` | count | Addresses returned |

A failing lookup is a measurement (`up = 0`), never a probe failure.

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
| `senhub.dns.up` | `senhub.dns.up` | DNS {name} via {resolver} Up | # | 1 when the lookup returned at least one answer within the timeout |
| `senhub.dns.lookup.duration` | `senhub.dns.lookup.duration` | DNS {name} via {resolver} Lookup Time | ms | Wall-clock resolution time |
| `senhub.dns.answers` | `senhub.dns.answers` | DNS {name} via {resolver} Answers | # | Number of addresses returned |

<!-- schema:metrics:end -->
