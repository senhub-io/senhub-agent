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
<!-- sha256:61c76d96be2746dc149f5ee9ece5c031157b801bd1dafeeb978e3ee9a03eb732 -->

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
| `oracle.sga.size` | By | System Global Area total size |
| `oracle.pga.size` | By | PGA memory currently allocated |
| `oracle.buffer.cache.hit_ratio` | % | Buffer cache hit ratio (data blocks found in memory) |
| `oracle.tablespace.used` | By | Tablespace space used per tablespace, tagged with `tablespace` |
| `oracle.tablespace.limit` | By | Tablespace total capacity |
| `oracle.wait_class.total` | # | Time waited per wait class in centiseconds, tagged with `wait_class` |
| `oracle.enqueue_deadlocks` | {deadlock} | Enqueue (row/table lock) deadlocks since instance start |

## Operational notes

- The probe needs a user that can open a session and read ten
  dictionary views. On a multitenant database, create it in the
  pluggable database the probe connects to, then, as a privileged user
  of that PDB:

    ```sql
    CREATE USER senhub IDENTIFIED BY "…";
    GRANT CREATE SESSION TO senhub;
    GRANT SELECT ON V_$INSTANCE TO senhub;
    GRANT SELECT ON V_$SESSION TO senhub;
    GRANT SELECT ON V_$RESOURCE_LIMIT TO senhub;
    GRANT SELECT ON V_$PARAMETER TO senhub;
    GRANT SELECT ON V_$SYSSTAT TO senhub;
    GRANT SELECT ON V_$SGASTAT TO senhub;
    GRANT SELECT ON V_$PGASTAT TO senhub;
    GRANT SELECT ON V_$SYSTEM_WAIT_CLASS TO senhub;
    GRANT SELECT ON DBA_TABLESPACES TO senhub;
    GRANT SELECT ON DBA_TABLESPACE_USAGE_METRICS TO senhub;
    ```

    The `V_$` names are the grantable objects behind the `V$` views.
    Inside a pluggable database `V$RESOURCE_LIMIT` has no rows, so the
    session limit is read from `V$PARAMETER` there.
    `GRANT SELECT_CATALOG_ROLE` covers all of them in one line if your
    policy allows it. A view the user cannot read leaves out the
    metrics that depend on it; the other metrics keep reporting.
- When the database is unreachable or refuses the login, the probe
  publishes `senhub.db.up = 0` and logs the reason (`ORA-01017` for a
  wrong password, `ORA-12514` for a service the listener does not know).
- Oracle Database 23ai accepts passwords of up to 1024 characters, where
  earlier releases stop at 30. The probe logs in with either: `go-ora`
  does not announce long password support on its own, and the probe adds
  that announcement, so a 23ai user with a generated 40 character
  password connects like any other. No server setting is needed.
  Before this was handled, such a login failed with `ORA-01017` while
  SQL\*Plus accepted the same credentials. Tested against Oracle
  Database 23ai Free (service `FREEPDB1`).
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
| `oracle.sga.size` | `oracle.sga.total` | SGA Total | B | Total SGA allocated in bytes (SUM(bytes) over v$sgastat) |
| `oracle.pga.size` | `oracle.pga.total` | PGA Total | B | Total PGA allocated in bytes (v$pgastat 'total PGA allocated') |
| `oracle.tablespace.used` | `oracle.tablespace.used` | Tablespace {tablespace} Used | B | Used space per tablespace in bytes (dba_tablespace_usage_metrics) |
| `oracle.tablespace.limit` | `oracle.tablespace.total` | Tablespace {tablespace} Total | B | Maximum size per tablespace in bytes (dba_tablespace_usage_metrics) |
| `oracle.wait_class.total` | `oracle.wait_class.total` | Wait Class {wait_class} | # | Cumulative time waited per wait class in centiseconds (v$system_wait_class) |
| `oracle.enqueue_deadlocks` | `oracle.enqueue_deadlocks` | Enqueue Deadlocks | # | Cumulative enqueue deadlocks detected (v$sysstat 'enqueue deadlocks') |

<!-- schema:metrics:end -->
