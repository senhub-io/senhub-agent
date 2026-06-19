<img src="https://cdn.simpleicons.org/consul" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Consul

The `consul` probe monitors a Consul agent and cluster, reporting catalog
service counts, Serf member counts, Raft commit latency, RPC and DNS counters,
health-check state distribution and leader status.

## Quick start

```yaml
probes:
  - name: consul
    type: consul
    params:
      endpoint: http://localhost:8500
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `http://localhost:8500` | Consul HTTP API base URL |
| `token` | — | Consul ACL token (required if ACLs are enabled) |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.consul.up` | 1 | 1 when the Consul HTTP API is reachable |
| `consul.catalog.services` | {service} | Number of services registered in the catalog |
| `consul.serf.members` | {member} | LAN Serf cluster members |
| `consul.raft.commit_time` | s | Median Raft commit latency |
| `consul.dns.latency` | s | Median DNS query latency |
| `consul.health.checks` | {check} | Health checks by state (passing/warning/critical), tagged with `state` |
| `consul.rpc.query.count` | {query} | RPC queries processed since last collection |
| `consul.leader` | 1 | 1 when this agent is the current Raft leader |

## Operational notes

- Without an ACL token, only metrics accessible to the anonymous token are visible. For full cluster observability, provide a token with at minimum `agent:read` and `catalog:read` policies.
- The probe queries `/v1/agent/metrics?format=prometheus` (Consul 1.1+), `/v1/agent/self` for leader state, and `/v1/health/state/*` for check counts.
