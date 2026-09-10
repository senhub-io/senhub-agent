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
# probes.d/10-prometheus-scrape.yaml — each file under probes.d/ is a YAML array of probes
- name: exporters
  type: prometheus_scrape
  params:
    targets:
      - "http://localhost:9100/metrics"      # node_exporter
      - "http://10.0.0.5:9117/metrics"       # appliance exporter
    interval: 60
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `targets` | Yes | - | Exposition URLs. Example: `http://localhost:9100/metrics` |
| `interval` | No | `60` | Seconds between scrapes |
| `timeout` | No | `10` | Whole-request budget in seconds |
| `metric_match` | No | - | Regular expression on metric family names |
| `bearer_token` | No | - | Sent as Authorization: Bearer. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `insecure_skip_verify` | No | `false` | Accept self-signed exporter certificates |

<!-- schema:params:end -->

The timeout applies to each target's request. Metric families that do not
match `metric_match` are skipped.

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
