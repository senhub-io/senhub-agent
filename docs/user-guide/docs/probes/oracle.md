<img src="https://cdn.simpleicons.org/oracle" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Oracle Database

The `oracle` probe monitors Oracle Database via `go-ora` (pure Go, no OCI
client required), collecting instance availability, session counts, SGA/PGA
memory, buffer cache hit ratio, tablespace usage, wait class statistics and
deadlock counts. Metric set targets parity with the community `oracledb_exporter`.

## Quick start

```yaml
# probes.d/20-oracle.yaml — each file under probes.d/ is a YAML array of probes
- name: oracle
  type: oracle
  params:
    host: db.example.com
    service_name: ORCL
    username: monitor
    password: ${secret:oracle.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `host` | Yes | - | Listener hostname or address. Example: `db.example.com` |
| `port` | No | `1521` | Listener port |
| `service_name` | Yes | - | Oracle service name, not the SID. Example: `ORCL` |
| `username` | Yes | - | Database user with SELECT on the v$ views |
| `password` | No | - | User's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `host` | required | Oracle Database server hostname or IP |
| `port` | `1521` | Oracle listener port |
| `service_name` | required | Oracle service name (not SID) |
| `username` | required | Database user with at least `SELECT` on v$ views |
| `password` | — | User password — reference a stored secret via `${secret:<name>.password}`, `${env:VAR}` or `${file:/path}`. Inline plaintext is auto-sealed into the OS secret store on install. |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | 1 | 1 when the agent reached the instance this cycle |
| `oracle.sessions.count` | {session} | Sessions by status (Active/Inactive), tagged with `status` |
| `oracle.sessions.limit` | {session} | Maximum allowed sessions |
| `oracle.sga.size` | By | System Global Area total size |
| `oracle.pga.allocated` | By | PGA memory currently allocated |
| `oracle.buffer_cache.hit_ratio` | % | Buffer cache hit ratio (data blocks found in memory) |
| `oracle.tablespace.usage` | By | Tablespace space used per tablespace, tagged with `tablespace` |
| `oracle.tablespace.capacity` | By | Tablespace total capacity |
| `oracle.wait_class.time` | s | DB time spent in each wait class, tagged with `wait_class` |
| `oracle.enqueue.deadlocks` | {deadlock} | Enqueue (row/table lock) deadlocks since instance start |

## Operational notes

- The minimum grant for the monitoring user: `GRANT CREATE SESSION TO monitor; GRANT SELECT ON V_$SESSION TO monitor;` plus similar grants on other v$ views used.
- No Oracle client (OCI) installation is needed — `go-ora` speaks the Oracle wire protocol directly.
- The probe connects using the service name, not the SID. Use `oracle://host:port/service_name` DSN shape internally.
