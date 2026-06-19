<img src="https://cdn.simpleicons.org/microsoftsqlserver" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Microsoft SQL Server

The `mssql` probe monitors SQL Server health and throughput via
`sys.dm_os_performance_counters`, `sys.databases` and
`sys.dm_io_virtual_file_stats`, with OTel-first metric naming aligned with
the OpenTelemetry Collector contrib `sqlserverreceiver`.

## Quick start

```yaml
probes:
  - name: mssql
    type: mssql
    params:
      host: localhost
      username: sa
      password: changeme
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `host` | required | SQL Server hostname or IP |
| `port` | `1433` | SQL Server TCP port |
| `username` | — | SQL Server login. Omit for Windows Integrated Authentication (the agent's OS account is used) |
| `password` | — | Password for SQL login |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.db.up` | 1 | 1 when the agent's most recent ping reached the server |
| `sqlserver.batch_request.rate` | {request}/s | Batch requests per second |
| `sqlserver.transaction.rate` | {transaction}/s | Transactions per second |
| `sqlserver.connections.open` | {connection} | Open connections |
| `sqlserver.buffer_cache_hit_ratio` | % | Buffer cache hit ratio (data pages found in memory) |
| `sqlserver.page.life_expectancy` | s | Estimated page life expectancy in the buffer pool |
| `sqlserver.lock.wait_time.avg` | ms | Average lock wait time |
| `sqlserver.deadlock.rate` | {deadlock}/s | Deadlocks per second |
| `sqlserver.database.state` | 1 | Database state per database (1 = ONLINE), tagged with `database` |
| `sqlserver.database.io.read` | By | I/O bytes read per database |
| `sqlserver.database.io.write` | By | I/O bytes written per database |

## Operational notes

- The monitoring account needs `VIEW SERVER STATE` and `VIEW DATABASE STATE` permissions. A minimal-privilege account is recommended over `sa`.
- Windows Integrated Authentication (omit `username`/`password`) works when the agent runs under a domain account with the required SQL Server permissions.
- For SQL Server on a named instance, use `host: server\InstanceName`.
