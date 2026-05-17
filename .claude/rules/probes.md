---
title: Probes — contract every probe must respect
paths:
  - internal/agent/probes/**
---

## Interface

Every probe implements `types.Probe` (defined in `internal/agent/probes/types/`). The required surface:

```go
type Probe interface {
    GetName() string
    GetProbeType() string
    GetInterval() time.Duration
    ShouldStart() bool
    OnStart(quitChannel chan struct{}) error
    Collect() ([]datapoint.DataPoint, error)
    OnShutdown(ctx context.Context) error
}
```

## Mandatory wiring — non-negotiable

1. **Embed `*types.BaseProbe`** in the probe struct:

   ```go
   type mysqlProbe struct {
       *types.BaseProbe
       // ...
   }
   ```

2. **Call `SetProbeType("…")` in the constructor** (`NewMySQLProbe`, `NewPostgreSQLProbe`, …). Without it, every metric ships without a `probe_type` tag and the cache layer warns at every cycle.

   ```go
   probe := &mysqlProbe{BaseProbe: &types.BaseProbe{}, ...}
   probe.SetProbeType("mysql")
   return probe, nil
   ```

3. **Wrap `Collect()` output with `EnrichDataPointsWithProbeName`** at the end:

   ```go
   enriched := p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName())
   return enriched, nil
   ```

   This adds `probe_name` and `probe_type` to every datapoint. Every other probe (Veeam, NetScaler, Citrix, Redfish) does this — new probes follow the same contract.

4. **Register in `internal/agent/probes/registry.go`** with the canonical type name. The registry name MUST match the YAML transformer file name (`mysql.yaml`, `postgresql.yaml`).

## commonTags shape

The standard per-probe tags emitted on every datapoint:

| Tag | Value | Notes |
|---|---|---|
| `metric_type` | family (overview / connections / throughput / cache / locks / io / storage / replication / archiver / bloat / stat_statements / per_database / per_table / …) | drives Sensor Builder chips |
| `probe_type` | engine name (mysql, postgresql, citrix, …) | set by `SetProbeType` |
| `instance` | `host:port` or equivalent unique target id | distinguishes multiple probe instances |
| `environment` | self_hosted / rds / aurora / cloudsql / azure_flexible / supabase / unknown | auto-detected when applicable |

For **DB probes** (mysql, postgresql), also emit the OTel-canonical resource attributes as metric tags so they flow to OTLP / Prom via `IncludeProbeTags`:

```go
{Key: "db.system.name", Value: "mysql"},
{Key: "server.address", Value: p.cfg.Host},
{Key: "server.port",    Value: strconv.Itoa(p.cfg.Port)},
```

## Metric naming

Internal probe metric names follow the OTel-first convention (see memory `feedback_otel_first.md`):

- Use canonical receiver names from `otelcol-contrib` when available (`mysql.uptime`, `postgresql.backends`, `mysql.threads`, …).
- Extend under `senhub.db.<engine>.*` (engine-specific) or `senhub.db.*` (cross-engine) when no contrib equivalent.
- **No unit suffix in metric names** (no `.seconds`, `.bytes`, `.ms`). Unit goes in the YAML `otel.unit` field.
- **No `.count` / `_total` suffix.** The type (counter vs gauge) carries that info.
- `system.*` is reserved for OTel system metrics (cpu/memory/disk/network). Don't put DB or app metrics there.

## Collapses via attributes

When multiple datapoints share semantics, prefer **one OTel name + discriminator attribute** over N separate metrics:

```go
// Good — collapsed via "kind" attribute:
p.addCountTagged(points, "mysql.threads.running",   threadsRunning,   now, MetricTypeConnections, "kind", "running")
p.addCountTagged(points, "mysql.threads.connected", threadsConnected, now, MetricTypeConnections, "kind", "connected")
```

Each `tag_to_attribute` in the YAML maps probe tags to OTel attributes. Add the discriminating tag key to `DiscriminantTagsRegistry` (`internal/agent/services/data_store/strategies/http/http_cache.go`) so cache keys stay unique.

## Lifecycle

- `OnStart`: open external connections (DB, HTTP client), capture version banners, fail fast on auth/credential issues. Connection failures here should return an error — the framework marks the probe unhealthy.
- `Collect`: one cycle. Ping/validate the connection before issuing queries (server restarts, idle timeouts). Emit datapoints even on partial failure (always emit `senhub.db.up` for DB probes).
- `OnShutdown`: close connections cleanly; cancel any in-flight context.

## License gating

Probes outside the free tier (`cpu, memory, logicaldisk, network, linux_logs`) must require a valid license token. The license check happens in the sensor service automatically — probe code doesn't need to call it directly. But the probe **type name** must be a deliberate, stable identifier because it's part of the license JWT claims.

## Tests

- Unit tests use synthetic input maps (stub `SHOW GLOBAL STATUS` etc.) — no real connection required for the per-family build* helpers.
- Integration tests live under `*_integration_test.go` with a `//go:build integration` tag (run via `make test-database`).
- Test assertions reference **OTel-canonical** metric names, not the legacy `db_*` form.
