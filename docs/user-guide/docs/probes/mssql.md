<img src="../../assets/probe-logos/mssql.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Microsoft SQL Server

The `mssql` probe monitors SQL Server health and throughput via
`sys.dm_os_performance_counters`, `sys.databases` and
`sys.dm_io_virtual_file_stats`, with OTel-first metric naming aligned with
the OpenTelemetry Collector contrib `sqlserverreceiver`.

## Quick start

```yaml
# probes.d/20-mssql.yaml — each file under probes.d/ is a YAML array of probes
- name: mssql
  type: mssql
  params:
    host: localhost
    username: sa
    password: ${secret:mssql.password}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `host` | Yes | - | Server hostname or address; use host\Instance for a named instance. Example: `sql01.example.com` |
| `port` | In practice | `1433` | Server TCP port |
| `username` | In practice | - | SQL login; empty selects Windows integrated authentication with the agent's account |
| `password` | In practice | - | SQL login password; empty with an empty username selects integrated authentication. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `encrypt` | No | `true` | Encryption of the connection, as go-mssqldb reads it; true by default. One of `true`, `false`, `disable`, `strict` |
| `trust_server_cert` | No | `false` | Accept the server certificate without verifying it |
| `interval` | No | `60` | Seconds between collections |

<!-- schema:params:end -->

- An `encrypt` value outside the four listed stops the probe at load time rather than reaching the server.
- `trust_server_cert: true` is for a lab, not for production.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | 1 | 1 when the agent's most recent ping reached the server |
| `sqlserver.batch_request.rate` | {request}/s | Batch requests per second |
| `sqlserver.transaction_rate` | {transaction}/s | Transactions per second |
| `sqlserver.user.connection.count` | {connection} | Open connections |
| `sqlserver.page_buffer_cache.hit_ratio` | % | Buffer cache hit ratio (data pages found in memory) |
| `sqlserver.page_life_expectancy` | s | Estimated page life expectancy in the buffer pool |
| `sqlserver.lock_wait_rate` | # | Lock requests per second that had to wait |
| `sqlserver.database.status` | 1 | Database state per database (1 = ONLINE), tagged with `database` |
| `sqlserver.database.io` | B | Bytes read and written per database, tagged with `direction` |

## Operational notes

- The monitoring account needs `VIEW SERVER STATE` and `VIEW DATABASE STATE` permissions. A minimal-privilege account is recommended over `sa`.
- Windows Integrated Authentication (omit `username`/`password`) works when the agent runs under a domain account with the required SQL Server permissions.
- For SQL Server on a named instance, use `host: server\InstanceName`.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.db.up` | `db_up` | # | 1 if the agent's most recent ping reached the server, 0 otherwise |
| `sqlserver.batch_request.rate` | `mssql_batch_request_rate` | # | T-SQL batch requests received per second (Batch Requests/sec) |
| `sqlserver.transaction_rate` | `mssql_transaction_rate` | # | Transactions started per second across all databases (Transactions/sec) |
| `sqlserver.page_buffer_cache.hit_ratio` | `mssql_page_buffer_cache_hit_ratio` | % | Percentage of page requests served from the buffer pool without a physical read |
| `sqlserver.page_life_expectancy` | `mssql_page_life_expectancy` | s | Seconds a page is expected to stay in the buffer pool (Page life expectancy) |
| `sqlserver.lock_wait_rate` | `mssql_lock_wait_rate` | # | Lock requests per second that required the caller to wait (Lock Waits/sec) |
| `sqlserver.processes.blocked` | `mssql_processes_blocked` | # | Number of currently blocked processes (Processes blocked) |
| `sqlserver.user.connection.count` | `mssql_user_connection_count` | # | Number of user connections to the instance (User Connections) |
| `sqlserver.database.io` | `mssql_database_io_{database}_{direction}` | B | Total bytes read/written per database since startup (sys.dm_io_virtual_file_stats) |
| `sqlserver.database.status` | `mssql_database_status_{database}` | # | Database state code (sys.databases.state — 0=ONLINE, 6=OFFLINE, …) |

<!-- schema:metrics:end -->
