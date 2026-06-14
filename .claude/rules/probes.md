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
    EntitySource() entity.Source
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

5. **Entity source** — call `SetEntitySource()` in the constructor for remote-target probes (see §Mandatory wiring — five touch-points below). Host-level probes and log conduits inherit `NoOpEntitySource` from `BaseProbe` automatically.

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

## Mandatory wiring — five touch-points, every new probe, same PR

When adding a probe, register it in the **five** places below in the **same PR**. The structural invariant tests in `internal/agent/probes/registry_invariant_test.go` make the license and entity source halves non-skippable — CI fails if either is missing. The other places are not test-enforced today but matter just as much.

1. **`internal/agent/probes/registry.go`** — add the entry to `probeConstructors`. The registry name MUST match the YAML transformer file name (`mysql.yaml`, `ibmi.yaml`).
2. **License authorization** — pick one:
   - **Free tier** → add to `freeTierProbes` in `internal/agent/services/license/license.go`. Only for **host-local** observability (cpu/memory/network/logicaldisk/linux_logs). Anything that monitors a remote system is paid.
   - **Paid (Pro/Enterprise)** → add the probe name to `paidProbes` in `internal/agent/services/license/probe_catalog.go` so a JWT licence is allowed to grant it via the `authorized_probes` claim. The structural test fails if a probe is wired in the registry without an entry here.
3. **`docs/LICENSE-SYSTEM.md`** — add the probe to the correct tier section (Free or Pro). The doc has drifted multiple times in the past; the structural test catches code drift, not doc drift.
4. **YAML transformer** at `internal/agent/services/data_store/transformers/definitions/<probe>.yaml` (unless the probe is a pure log conduit like `linux_logs` — in that case document the absence in `senhub-semantic-conventions.md`). Every metric in the YAML needs an `otel:` block (see `feedback_otel_first.md`); the prometheus mapper warns once per unmapped metric and silently drops, so a missing block ships as a silent feature gap.
5. **Entity source** in the probe's constructor — non-negotiable, enforced by
   `TestEveryRegisteredProbeHasEntitySource` in `registry_invariant_test.go`.

   **Remote-target probes** (anything monitoring a distinct external system — a DB instance, a message broker, an HTTP endpoint): call `SetEntitySource()` with a `SimpleEntitySource` in the constructor. The invariant test catches a nil return; `SimpleEntitySource` satisfies it:

   ```go
   // In the probe's constructor:
   entitySrc := types.NewSimpleEntitySource("db.redis", map[string]any{
       "server.address": cfg.Host,
       "server.port":    int64(cfg.Port),
   })
   p.SetEntitySource(entitySrc)

   // In OnStart():
   p.unregister = entity.RegisterSource(p.EntitySource())

   // In Collect(), after success:
   entitySrc.SetUp(true, map[string]any{"db.system.version": version})
   // On failure:
   entitySrc.SetUp(false, nil)

   // In OnShutdown():
   if p.unregister != nil { p.unregister() }
   ```

   **Host-level probes and log conduits** (cpu, memory, network, logicaldisk, linux_logs, syslog, filetail, windowseventlog, event): do NOT call `SetEntitySource()`. They inherit the `NoOpEntitySource` fallback from `BaseProbe`, which satisfies the invariant without emitting extra entity events — the host entity is already reported by the entity detector.

   Entity type taxonomy (immutable — changing a type is a breaking change for Toise):

   | Technology | Entity type | Immutable ID keys |
   |---|---|---|
   | Redis/Valkey | `db.redis` | `server.address`, `server.port` |
   | MongoDB | `db.mongodb` | `server.address`, `server.port` |
   | Cassandra | `db.cassandra` | `server.address`, `server.port` |
   | CouchDB | `db.couchdb` | `server.address`, `server.port` |
   | InfluxDB | `db.influxdb` | `server.address`, `server.port` |
   | ClickHouse | `db.clickhouse` | `server.address`, `server.port` |
   | Memcached | `db.memcached` | `server.address`, `server.port` |
   | Elasticsearch | `search.engine` | `server.address`, `server.port`, `search.engine.type=elasticsearch` |
   | OpenSearch | `search.engine` | `server.address`, `server.port`, `search.engine.type=opensearch` |
   | Solr | `search.engine` | `server.address`, `server.port`, `search.engine.type=solr` |
   | Nginx | `web.server` | `server.address`, `server.port`, `web.server.type=nginx` |
   | Apache | `web.server` | `server.address`, `server.port`, `web.server.type=apache` |
   | HAProxy | `load_balancer` | `server.address`, `server.port` |
   | Envoy | `proxy` | `server.address`, `server.port` |
   | Varnish | `cache.server` | `server.address`, `server.port` |
   | RabbitMQ | `messaging.broker` | `server.address`, `server.port`, `messaging.system=rabbitmq` |
   | Kafka | `messaging.broker` | `server.address`, `server.port`, `messaging.system=kafka` |
   | ActiveMQ | `messaging.broker` | `server.address`, `server.port`, `messaging.system=activemq` |
   | Pulsar | `messaging.broker` | `server.address`, `server.port`, `messaging.system=pulsar` |
   | Tomcat | `app.server` | `server.address`, `server.port`, `app.server.type=tomcat` |
   | WildFly | `app.server` | `server.address`, `server.port`, `app.server.type=wildfly` |
   | Zookeeper | `coordination.service` | `server.address`, `server.port` |
   | Consul | `service_mesh.node` | `server.address`, `server.port` |
   | PHP-FPM | `runtime.php_fpm` | `server.address`, `server.port` |
   | Docker container | `container` | `container.id` |
   | SNMP device | `network.device` | (managed by snmppoll — already implemented) |
   | Host | `host` | (managed by entity detector — already implemented) |

For probes that emit collapsed metrics (one OTel name + discriminator attribute), also add the discriminator tag key to `DiscriminantTagsRegistry["<probe>"]` in `internal/agent/services/data_store/strategies/http/http_cache.go`. Without this, the cache key collapses all variants onto one slot.

The probe **type name** must be a deliberate, stable identifier — it's part of license JWT claims, transformer file paths, `DiscriminantTagsRegistry` keys, and customer JWTs already in the wild. Renaming a probe type is a breaking change for every customer holding a license that names it.

## Tests

- Unit tests use synthetic input maps (stub `SHOW GLOBAL STATUS` etc.) — no real connection required for the per-family build* helpers.
- Integration tests live under `*_integration_test.go` with a `//go:build integration` tag (run via `make test-database` in senhub-agent-enterprise, where the database probes live).
- Test assertions reference **OTel-canonical** metric names, not the legacy `db_*` form.
