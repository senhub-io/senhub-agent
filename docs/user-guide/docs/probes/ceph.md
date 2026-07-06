<img src="https://cdn.simpleicons.org/ceph" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Ceph

The `ceph` probe monitors a Ceph cluster via the Ceph REST Management API v1,
reporting cluster health, OSD counts, monitor quorum, capacity and per-pool
I/O statistics.

## Quick start

```yaml
# probes.d/10-ceph.yaml — each file under probes.d/ is a YAML array of probes
- name: ceph
  type: ceph
  params:
    endpoint: https://localhost:8443
    username: admin
    password: ${secret:ceph.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `endpoint` | `https://localhost:8443` | Base URL of the Ceph Manager Dashboard / REST API |
| `username` | — | Ceph dashboard username (required) |
| `password` | — | Ceph dashboard password (required) — reference via `${secret:ceph.password}`, `${env:VAR}` or `${file:/path}`; inline plaintext is auto-sealed into the OS secret store on install |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.ceph.up` | 1 | 1 when the Ceph REST API is reachable and authentication succeeds |
| `ceph.health.status` | 1 | Cluster health: 2 = HEALTH_OK, 1 = HEALTH_WARN, 0 = HEALTH_ERR |
| `ceph.cluster.capacity` | By | Raw cluster storage capacity |
| `ceph.cluster.used` | By | Raw storage space in use |
| `ceph.osd.total` | {osd} | Total number of OSDs in the cluster |
| `ceph.osd.up` | {osd} | OSDs currently in the `up` state |
| `ceph.osd.in` | {osd} | OSDs currently in the `in` state (participating in data placement) |
| `ceph.monitor.quorum` | {monitor} | Number of monitors participating in the quorum |
| `ceph.pool.reads` | {read} | Read operations per pool (tagged with `pool`) |
| `ceph.pool.writes` | {write} | Write operations per pool |
| `ceph.pool.bytes_read` | By | Bytes read per pool |
| `ceph.pool.bytes_written` | By | Bytes written per pool |

## Operational notes

- The Ceph Manager Dashboard must be enabled: `ceph mgr module enable dashboard`.
- The default endpoint uses a self-signed TLS certificate; the probe skips verification by default. For production, configure a proper certificate.
- The API requires Ceph Nautilus (14+) or newer for the `/api/` v1 interface.
