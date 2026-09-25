<img src="../../assets/probe-logos/oracle.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | Yes | - | Listener hostname or address. Example: `db.example.com` |
| `port` | In practice | `1521` | Listener port |
| `service_name` | Yes | - | Oracle service name, not the SID. Example: `ORCL` |
| `username` | Yes | - | Database user with SELECT on the v$ views |
| `password` | In practice | - | User's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `interval` | No | `60` | Seconds between collections |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | 1 | 1 when the agent reached the instance this cycle |
| `oracle.sessions.count` | {session} | Sessions by status (Active/Inactive), tagged with `status` |
| `oracle.sessions.limit` | {session} | Maximum allowed sessions |
| `oracle.sga.total` | By | System Global Area total size |
| `oracle.pga.total` | By | PGA memory currently allocated |
| `oracle.buffer.cache.hit_ratio` | % | Buffer cache hit ratio (data blocks found in memory) |
| `oracle.tablespace.used` | By | Tablespace space used per tablespace, tagged with `tablespace` |
| `oracle.tablespace.total` | By | Tablespace total capacity |
| `oracle.wait_class.total` | # | Time waited per wait class in centiseconds, tagged with `wait_class` |
| `oracle.enqueue_deadlocks` | {deadlock} | Enqueue (row/table lock) deadlocks since instance start |

## Operational notes

- The minimum grant for the monitoring user: `GRANT CREATE SESSION TO monitor; GRANT SELECT ON V_$SESSION TO monitor;` plus similar grants on other v$ views used.
- No Oracle client (OCI) installation is needed — `go-ora` speaks the Oracle wire protocol directly.
- The probe connects using the service name, not the SID.

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
| `senhub.db.up` | `senhub.db.up` | Database Up | # | 1 if the agent's most recent ping reached the instance, 0 otherwise |
| `oracle.sessions.count` | `oracle.sessions.count` | Sessions {status} | # | Open sessions by status (v$session GROUP BY status) |
| `oracle.sessions.limit` | `oracle.sessions.limit` | Sessions Limit | # | Configured maximum sessions (v$resource_limit, resource_name='sessions') |
| `oracle.physical.reads` | `oracle.physical.reads` | Physical Reads | # | Cumulative physical reads (v$sysstat 'physical reads') |
| `oracle.physical.writes` | `oracle.physical.writes` | Physical Writes | # | Cumulative physical writes (v$sysstat 'physical writes') |
| `oracle.buffer.cache.hit_ratio` | `oracle.buffer.cache.hit_ratio` | Buffer Cache Hit Ratio | % | 1 - physical reads / (consistent gets + db block gets), derived from v$sysstat |
| `oracle.sga.total` | `oracle.sga.total` | SGA Total | B | Total SGA allocated in bytes (SUM(bytes) over v$sgastat) |
| `oracle.pga.total` | `oracle.pga.total` | PGA Total | B | Total PGA allocated in bytes (v$pgastat 'total PGA allocated') |
| `oracle.tablespace.used` | `oracle.tablespace.used` | Tablespace {tablespace} Used | B | Used space per tablespace in bytes (dba_tablespace_usage_metrics) |
| `oracle.tablespace.total` | `oracle.tablespace.total` | Tablespace {tablespace} Total | B | Maximum size per tablespace in bytes (dba_tablespace_usage_metrics) |
| `oracle.wait_class.total` | `oracle.wait_class.total` | Wait Class {wait_class} | # | Cumulative time waited per wait class in centiseconds (v$system_wait_class) |
| `oracle.enqueue_deadlocks` | `oracle.enqueue_deadlocks` | Enqueue Deadlocks | # | Cumulative enqueue deadlocks detected (v$sysstat 'enqueue deadlocks') |

<!-- schema:metrics:end -->
