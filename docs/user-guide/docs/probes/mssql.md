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

| Parameter | Required | Default | Description |
|---|---|---|---|
| `host` | Yes | - | Server hostname or address; use host\Instance for a named instance. Example: `sql01.example.com` |
| `port` | No | `1433` | Server TCP port |
| `username` | No | - | SQL login; empty selects Windows integrated authentication with the agent's account |
| `password` | No | - | SQL login password; empty with an empty username selects integrated authentication. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
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
