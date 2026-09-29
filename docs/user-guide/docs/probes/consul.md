<img src="../../assets/probe-logos/consul.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Consul

The `consul` probe monitors a Consul agent and cluster, reporting catalog
service counts, Serf member counts, Raft commit latency, RPC and DNS counters,
health-check state distribution and leader status.

## Quick start

```yaml
# probes.d/10-consul.yaml — each file under probes.d/ is a YAML array of probes
- name: consul
  type: consul
  params:
    endpoint: http://localhost:8500
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:b18d4b5097649fe985ac5f628e36fce119d5e49470d189198a743370d42b6062 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | In practice | `http://localhost:8500` | Base URL of the Consul HTTP API. Example: `http://consul.example.com:8500` |
| `token` | No | - | ACL token sent with every request; empty when ACLs are disabled. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `timeout` | No | `10` | Request timeout in seconds |
| `interval` | No | `30` | Seconds between collections |
| `instance_name` | No | - | Stable identity of this agent instead of the node id it reports |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.consul.up` | 1 | 1 when the Consul HTTP API is reachable |
| `consul.catalog.services` | {service} | Number of services registered in the catalog |
| `consul.serf.members` | {member} | LAN Serf cluster members |
| `consul.raft.commit.time` | ms | Mean Raft commit time over the last interval |
| `consul.dns.queries` | # | DNS domain queries handled by this agent (cumulative) |
| `consul.health.checks` | {check} | Health checks by state (passing/warning/critical), tagged with `state` |
| `consul.rpc.requests` | # | RPC requests handled by this agent (cumulative) |
| `consul.leader` | 1 | 1 when this agent is the current Raft leader |

## Operational notes

- Without an ACL token, only metrics accessible to the anonymous token are visible. For full cluster observability, provide a token with at minimum `agent:read` and `catalog:read` policies.
- The probe queries `/v1/agent/metrics?format=prometheus` (Consul 1.1+), `/v1/agent/self` for leader state, and `/v1/health/state/*` for check counts.

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
| `senhub.consul.up` | `senhub.consul.up` | Consul Up | # | 1 when the Consul agent HTTP API is reachable and responding |
| `consul.catalog.services` | `consul.catalog.services` | Consul Registered Services | # | Number of services registered in the Consul catalog |
| `consul.serf.members` | `consul.serf.members` | Consul LAN Members | # | Number of LAN Serf cluster members |
| `consul.raft.commit.time` | `consul.raft.commit.time` | Consul Raft Commit Time | ms | Mean Raft commit time over the last interval (milliseconds) |
| `consul.rpc.requests` | `consul.rpc.requests` | Consul RPC Requests | # | Total RPC requests handled by this Consul agent |
| `consul.dns.queries` | `consul.dns.queries` | Consul DNS Queries | # | Total DNS domain queries handled by this Consul agent |
| `consul.health.checks` | `consul.health.checks` | Consul Health Checks ({state}) | # | Number of Consul health checks in this state (critical, warning, passing) |
| `consul.leader` | `consul.leader` | Consul Leader | # | 1 when this Consul agent is the current Raft leader |

<!-- schema:metrics:end -->
