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

## License touch-points — every new probe MUST update all four

When adding a probe, register it in the **four** places below in the **same PR**. The structural invariant test `TestEveryRegisteredProbeIsAuthorizable` in `internal/agent/probes/registry_invariant_test.go` makes the license half non-skippable — CI fails if you wire the probe in `registry.go` without claiming a license seat. The other places are not test-enforced today but matter just as much.

1. **`internal/agent/probes/registry.go`** — add the entry to `probeConstructors`. The registry name MUST match the YAML transformer file name (`mysql.yaml`, `ibmi.yaml`).
2. **License authorization** — pick one:
   - **Free tier** → add to `freeTierProbes` in `internal/agent/services/license/license.go`. Only for **host-local** observability (cpu/memory/network/logicaldisk/linux_logs). Anything that monitors a remote system is paid.
   - **Paid (Pro/Enterprise)** → claim the next free slot in `probeBitmap` in `internal/agent/services/license/compact.go` so the compact-license format can authorize it. 13 slots free at the time of writing; reserved slots leave a comment in place. JWT-based licenses also need this entry so `authorized_probes` array entries match the canonical names.
3. **`docs/LICENSE-SYSTEM.md`** — add the probe to the correct tier section (Free or Pro). The doc has drifted multiple times in the past; the structural test catches code drift, not doc drift.
4. **YAML transformer** at `internal/agent/services/data_store/transformers/definitions/<probe>.yaml` (unless the probe is a pure log conduit like `linux_logs` — in that case document the absence in `senhub-semantic-conventions.md`). Every metric in the YAML needs an `otel:` block (see `feedback_otel_first.md`); the prometheus mapper warns once per unmapped metric and silently drops, so a missing block ships as a silent feature gap.

For probes that emit collapsed metrics (one OTel name + discriminator attribute), also add the discriminator tag key to `DiscriminantTagsRegistry["<probe>"]` in `internal/agent/services/data_store/strategies/http/http_cache.go`. Without this, the cache key collapses all variants onto one slot.

The probe **type name** must be a deliberate, stable identifier — it's part of license JWT claims, transformer file paths, `DiscriminantTagsRegistry` keys, and customer JWTs already in the wild. Renaming a probe type is a breaking change for every customer holding a license that names it.

## Tests

- Unit tests use synthetic input maps (stub `SHOW GLOBAL STATUS` etc.) — no real connection required for the per-family build* helpers.
- Integration tests live under `*_integration_test.go` with a `//go:build integration` tag (run via `make test-database`).
- Test assertions reference **OTel-canonical** metric names, not the legacy `db_*` form.
