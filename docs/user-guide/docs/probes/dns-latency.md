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

| Parameter | Default | Description |
|---|---|---|
| `names` | required | Names to resolve |
| `resolvers` | system | DNS servers (`ip` or `ip:port`); each name is measured against each resolver |
| `timeout` | 5 | Per-lookup budget in seconds |
| `interval` | 60 | Seconds between cycles |

## Metrics

One series per (name, resolver) pair — `resolver` is `system` when no
explicit servers are configured.

| Metric | Unit | Description |
|---|---|---|
| `senhub.dns.up` | bool | Lookup answered within the timeout |
| `senhub.dns.lookup.duration` | ms | Resolution time (only when up) |
| `senhub.dns.answers` | count | Addresses returned |

A failing lookup is a measurement (`up = 0`), never a probe failure.
