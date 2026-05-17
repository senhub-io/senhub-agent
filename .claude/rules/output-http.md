---
title: HTTP output — Prometheus / Nagios / PRTG / Zabbix (pull formats)
paths:
  - internal/agent/services/data_store/strategies/http/**
  - internal/agent/services/data_store/strategies/prtg/**
---

## What this is

The `http` strategy is the **unified HTTP server** that exposes the agent's cache to a family of pull-based monitoring tools. One Go HTTP server, multiple sub-formats:

| Sub-format | Path | Status |
|---|---|---|
| PRTG | `/api/{agentkey}/prtg/metrics/{probe}` | Primary supported |
| Nagios | `/api/{agentkey}/nagios/metrics/{probe}`, `/nagios/metrics`, `/nagios/checks` | Primary supported |
| Prometheus | `/api/{agentkey}/prometheus/metrics`, `/metrics` | In progress (cf. memory `feedback_prtg_only.md`) — feature-flagged but stable; expose to users with care |
| Zabbix | `/api/{agentkey}/zabbix/metrics/{probe}` | **Work starting 2026-05-17** — endpoint exists, full integration in progress |
| Web UI | `/web/{agentkey}/...` | Dashboard, Sensor Builder, Docs |
| Lookups | `/api/{agentkey}/lookups/prtg`, `/lookups/prtg/{id}` | PRTG `.ovl` files |
| Discovery | `/api/{agentkey}/endpoints`, `/info/probes`, `/info/tags/{probe}` | Self-introspection |

Each sub-format is gated by `IsEndpointEnabled("<name>")` (see `http_handlers.go`). Disable any of them via the `endpoints: [...]` list in the storage YAML.

## Auth model

- Every authenticated route includes `{agentkey}` in the URL path. The handler validates against the agent's current key.
- A few utility routes (`/health`, the bare `/metrics` for plain Prometheus) use Bearer-or-query authentication via `AuthenticateBearerOrQuery`.
- Never expose a route that returns data without auth. New routes go behind `{agentkey}` by default.

## Mapper-side OTel conformance (semconv §2bis)

All sub-formats consume from the **shared otelmapper**. Each one serializes the same OtelRecord differently:

- **Prometheus**: dotted→underscore, type-suffixed (`_total` on counters, `_ratio` on `unit:"1"` gauges, `_seconds`/`_bytes` per OTel→Prom unit table). Labels = OtelRecord attributes.
- **Nagios**: text line with `OK|WARN|CRIT|UNKNOWN - message | perfdata` — perfdata keys are sanitized OtelRecord attribute joins.
- **PRTG**: JSON `{"prtg":{"result":[{"channel":..., "value":..., "float":1, "unit":"..."}, ...]}}` — channel name from YAML `display_name` (with `{tag}` template substitution).
- **Zabbix**: format TBD (work starting). Will follow Zabbix's HTTP agent JSON conventions.

**Don't reinvent shapes per sub-format** when the OtelRecord already carries the semantics. Add unit conversion or label massaging to the mapper, not to the sink.

## Lookups (PRTG)

Lookup files (`.ovl`) translate enum integers to text in the PRTG sensor display. They live under `strategies/http/lookups/`. To add one:

1. Drop the `.ovl` file in the directory (must be valid PRTG lookup XML).
2. Reference its id (without `.ovl`) in the YAML transformer field `lookup:`.
3. The handler serves it automatically at `/api/{agentkey}/lookups/prtg/{id}`.

## Multi-instance label expansion

When a metric uses templated channel names (`channel: "mysql_commands_{command}"`), the YAML must declare `multi_instance_labels: ["command"]`. Without it, the `{command}` placeholder is emitted literally and you get one channel for everything instead of one per verb.

## DiscriminantTagsRegistry

The cache key generation uses `DiscriminantTagsRegistry` (`http_cache.go`) to know which tags differentiate distinct time series. When you add a new probe or a new tag-discriminated metric:

- Add the probe type and its discriminating tag keys to the registry.
- WITHOUT this, multiple datapoints under one metric name (e.g. `mysql.commands` with `command=select|insert|...`) collapse to a single cache slot and all-but-last is lost.

## Web UI

The web dashboard (`/web/{agentkey}/...`) is a separate sub-system covered by `project_web_ui_refactoring` memory entry. Backend changes here may need a frontend coordination. When you change a public endpoint shape, check whether the web UI consumes it.

## Common pitfalls

- **Empty channel list on PRTG**: probe didn't emit anything yet (first cycle), or the probe is mis-registered (probe_type in registry doesn't match the YAML `probe_name`).
- **Prometheus dashboard query returns nothing**: the metric name suffix is the auto-derived one (e.g. `senhub_mysql_uptime_seconds_total`), not the bare OTel name. See the Prometheus mapping rules in `senhub-semantic-conventions.md` §2.
- **Nagios perfdata key conflicts**: sanitization joins tags with underscores; two distinct OtelRecord attribute sets can collide after sanitization. Add a tag to the discriminant or the test will catch it.
