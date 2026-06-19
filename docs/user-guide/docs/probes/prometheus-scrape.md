<img src="https://cdn.simpleicons.org/prometheus" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Prometheus Scrape Probe

The `prometheus_scrape` probe pulls Prometheus `/metrics` endpoints —
node_exporter, appliance exporters, any application exposing the text
format — and ingests the samples through the agent pipeline, out to
every configured output. It is the pull-side twin of the
[OTLP receiver](otlp-receiver.md): one agent per site collects both
push (OTLP) and pull (Prometheus) sources.

## Quick start

```yaml
probes:
  - name: exporters
    type: prometheus_scrape
    params:
      targets:
        - "http://localhost:9100/metrics"      # node_exporter
        - "http://10.0.0.5:9117/metrics"       # appliance exporter
      interval: 60
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `targets` | required | List of exposition URLs |
| `interval` | `60` | Seconds between scrape cycles |
| `timeout` | `10` | Per-target budget in seconds |
| `metric_match` | none | Regexp filter on metric family names; non-matching families are skipped |
| `bearer_token` | none | Sent as `Authorization: Bearer ...`; use `${env:...}` substitution |
| `insecure_skip_verify` | `false` | Accept self-signed exporter certificates |

Targets are scraped in parallel (bounded). An unreachable exporter is
a measurement (`senhub.promscrape.up = 0`), never a probe failure.

## Behavior

- **Pass-through naming.** Scraped metric names and labels are
  forwarded unchanged; counter/gauge semantics are preserved.
  Untyped samples are treated as gauges.
- **Scalars only.** Counter, gauge and untyped series are ingested.
  Histogram and summary series are dropped and counted in
  `senhub.promscrape.dropped` — the same contract as the OTLP
  receiver.
- **Size cap.** A scrape response is read up to 32 MiB; a runaway
  exporter cannot exhaust the agent's memory.

## Self-metrics

One series per target (`target` tag).

| Metric | Description |
|---|---|
| `senhub.promscrape.up` | 1 when the target answered with a parseable exposition |
| `senhub.promscrape.scrape.duration` | Scrape wall-clock time |
| `senhub.promscrape.samples` | Scalar series ingested in the last scrape |
| `senhub.promscrape.dropped` | Histogram/summary series dropped in the last scrape |

## Operational notes

- **Use `metric_match` on big exporters.** A full node_exporter
  exposes 1000+ series; `metric_match: "^node_(cpu|memory|filesystem|network)"`
  keeps cardinality under control.
- **Re-exporting to Prometheus.** Scraped series flowing back out the
  agent's own Prometheus endpoint are prefixed `senhub_`, so they
  never collide with a direct scrape of the same exporter.
- **PRTG / Nagios outputs.** These sinks key series per target;
  finer per-label splits are carried on the Prometheus and OTLP
  outputs.
