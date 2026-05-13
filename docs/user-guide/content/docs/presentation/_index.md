---
title: "Presentation"
weight: 1
---

# Presentation

A single agent, deployed on a server you control, that turns the
state of your infrastructure into clean, queryable telemetry. It
collects metrics, status changes and logs from operating systems,
backup software, hyperconverged appliances, virtualization stacks
and network gear — then exposes them through whichever monitoring
or observability tool you already operate.

## In one paragraph

You install a single binary on a host that has network access to
the systems you want to monitor. A YAML file declares which probes
the agent should run and where the values should go (PRTG / Nagios
HTTP endpoints, Prometheus `/metrics` scrape page, OTLP/gRPC push).
The agent starts collecting on its own schedule and stays out of
the way — restartable, observable, and runnable in air-gapped
environments where no callback to the SenHub backend is allowed.

## What the agent does

- **One collector for many sources.** Each *probe* knows how to
  talk to one class of system (Veeam, Citrix, NetScaler, Redfish,
  Linux/Windows hosts, …). You add probes to the agent's YAML; you
  do not glue together vendor exporters.
- **One vocabulary across every output.** Probes emit values into
  a shared in-memory cache. From there the agent serves PRTG /
  Nagios HTTP endpoints, exposes a Prometheus `/metrics` scrape
  page, and natively pushes OTLP/gRPC metrics and logs to any
  OpenTelemetry receiver — collector, vmagent, Tempo, Grafana
  Cloud OTLP. **Metric names, units and attributes match across
  all sinks**, so a query that works in Grafana today keeps working
  in your PRTG sensor template tomorrow.
- **Tiered licensing.** A free tier ships the OS probes (CPU,
  memory, disk, network, Linux logs). The Pro and Enterprise tiers
  unlock the vendor probes — Citrix, NetScaler, Redfish, Veeam,
  syslog, custom events, network synthetic checks.
- **Self-observability built in.** Process resources, OTLP push
  health, collect-cycle counters and a `system.processes.count`
  metric are emitted on every output so the agent stays
  observable in the same panel as the systems it watches.

## What the agent does NOT do

- It does not ship dashboards by itself. Grafana dashboards
  shipped under `docs/grafana/` are a separate catalog you can
  import; PRTG sensor templates are generated from the agent's
  channel definitions on the fly.
- It does not host long-term storage. Metrics, logs and traces
  live in your own Prometheus / VictoriaMetrics / Tempo / Grafana
  Cloud backend.
- It is not a router or proxy. Probes connect outbound to the
  systems they query; receivers like `syslog` accept inbound
  traffic but do not relay it elsewhere unless an OTLP push
  storage is configured.

## Output paths at a glance

| Path | Protocol | Use case |
|---|---|---|
| `/api/{key}/prtg/metrics/{probe}` | HTTPS, JSON | Native PRTG sensor templates (Sensor Builder) |
| `/api/{key}/nagios/metrics/{probe}` | HTTPS, text | NRPE / Nagios performance line |
| `/api/{key}/prometheus/metrics` | HTTPS, text exposition | Prometheus / VictoriaMetrics scrape |
| OTLP gRPC client | gRPC, mTLS | Push to OTel collector, vmagent, Tempo, Grafana Cloud OTLP |

All four read from the same in-memory metric cache so the data is
consistent regardless of which sinks you enable.

## Where to next

- [Installation]({{< relref "/docs/installation" >}}) — install the binary on Windows or Linux
- [Configuration]({{< relref "/docs/configuration" >}}) — write the YAML
- [Probes]({{< relref "/docs/probes" >}}) — browse the probe catalog by vendor family
- [Prometheus / VictoriaMetrics]({{< relref "/docs/prometheus" >}}) — scrape configuration and metrics reference
- [OTLP / OpenTelemetry]({{< relref "/docs/otlp" >}}) — push metrics + logs to an OTel receiver
