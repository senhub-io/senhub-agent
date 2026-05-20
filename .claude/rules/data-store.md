---
title: Data store — the bus between probes and outputs
paths:
  - internal/agent/services/data_store/*.go
  - internal/agent/services/data_store/otelmapper/**
  - internal/agent/services/data_store/transformers/**
  - internal/agent/services/data_store/agentmetrics/**
---

## The pipeline

```
   Probes ──► DataStore ──► Strategies
              │
              ├── buffer.go (Append / Sync / AbortSync)
              ├── otelmapper/ (neutral OTel mapper, shared by Prom + OTLP)
              └── transformers/ (per-probe YAML definitions, formats, lookups)
```

Every probe pushes datapoints through the same `DataStore.AddDataPoints(...)` entry. The store fans out to enabled strategies. Strategies never see each other; they only see datapoints + the neutral mapper.

## otelmapper is neutral — keep it that way

`internal/agent/services/data_store/otelmapper/` (`resolve.go`, `convert.go`, `types.go`) is **the shared OTel mapping layer**. It is consumed by:

- The Prometheus exposition (`strategies/http/prometheus/`).
- The OTLP push (`strategies/otlp/`).
- Future native exporters (Zabbix is the next target — same neutral path).

Rules for the mapper:

- **No sink-specific code in `otelmapper`.** No HTTP, no gRPC, no proto types. It outputs `OtelRecord` structs; the sinks serialize.
- **No probe-specific code in `otelmapper`.** It reads probe definitions through the `transformers` package and applies them uniformly.
- The mapper handles `otel.expand` for enum metrics (1 cache datapoint → N per-state OTel records) and unit conversions (`%` → ratio, `ms` → `s`, `MB` → `By`).

## Transformer YAML format v3

Each probe has a YAML file at `internal/agent/services/data_store/transformers/definitions/<probe>.yaml`. Format v3 fields per metric:

| Field | Purpose |
|---|---|
| `name` | Internal probe-emitted name. MUST match what `Collect()` writes. |
| `channel` | PRTG channel ID (legacy short form, may include `{tag}` templates). |
| `display_name` | Operator-facing label for PRTG / Nagios (`{tag}` templates supported). |
| `unit` | Operator-facing unit (`%`, `#`, `s`, `ms`, `B`, …). |
| `multi_instance_labels` | List of tags whose values get substituted into `{xxx}` placeholders. **Required for any templated `channel:` or `display_name:`.** |
| `category` | Sensor Builder family chip. |
| `description` | Free-form, surfaced in Sensor Builder + Prom HELP. |
| `lookup` | Lookup file id (without `.ovl`) when the value is an enum that PRTG should resolve. |
| `otel.name` | Canonical OTel metric name. |
| `otel.unit` | OTel unit (`s`, `By`, `1`, `{thread}`, …). |
| `otel.type` | `counter`, `gauge`, `updowncounter`. |
| `otel.attributes` | Static attributes added to every series of this metric. |
| `otel.expand` | Declares an enum expansion (mapper produces N records). |
| `tag_to_attribute` | Maps probe tag keys to OTel attribute keys (renames). |

Multiple internal `name:` entries can share the same `otel.name` for collapse-via-attribute patterns (see `probes.md` rule).

## Lookups

Lookup files live under `internal/agent/services/data_store/strategies/http/lookups/`. They are downloaded by PRTG to map enum integers to human-readable text. Add a new lookup:

1. Drop the `.ovl` file in `lookups/`.
2. Reference its id in the YAML transformer (`lookup: senhub.db.up`).
3. The HTTP strategy serves it at `/api/{agentkey}/lookups/prtg/{lookup_id}` automatically.

## Buffer contract

`buffer.go` provides an in-memory ring used by the cloud (senhub) strategy. The contract:

```go
type Buffer interface {
    Append(points []datapoint.DataPoint) error
    Sync() []datapoint.DataPoint
    AbortSync(failedData []datapoint.DataPoint) error
}
```

- `Sync` returns the current batch and clears it.
- `AbortSync` puts the failed batch back at the head — the next `Sync` will include it again.
- Strategies that need batching consume this; non-batching strategies (PRTG pull, OTLP per-cycle push) read from the cache directly.

## DiscriminantTagsRegistry

When a probe emits multiple datapoints under the same internal metric name discriminated by a tag (e.g. `mysql.commands` with verb in `command`), the tag MUST appear in `DiscriminantTagsRegistry` in `strategies/http/http_cache.go`. Otherwise the cache key collapses all variants onto one slot and you lose all but the last value.

When you add a new collapsed metric, update both the YAML and the registry in the same commit.
