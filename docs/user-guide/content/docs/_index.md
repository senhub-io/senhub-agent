---
title: "SenHub Agent"
type: docs
---

# SenHub Agent

A single agent, deployed on a server you control, that turns the
state of your infrastructure into clean, queryable telemetry. It
collects metrics, status changes and logs from operating systems,
backup software, hyperconverged appliances, virtualization stacks
and network gear — then exposes them through whichever monitoring
or observability tool you already operate.

## What it does

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
- **Two operating modes.** *Offline* runs entirely from a local
  YAML, no callback to SenHub. *Online* periodically syncs probe
  configuration from the SenHub backend so probes can be added or
  retuned without touching the box.
- **Tiered licensing.** A free tier ships the OS probes (CPU,
  memory, disk, network, Linux logs). The Pro and Enterprise tiers
  unlock the vendor probes — Citrix, NetScaler, Redfish, Veeam,
  syslog, custom events, network synthetic checks.
- **Self-observability built in.** Process resources, OTLP push
  health, collect-cycle counters and a `system.processes.count`
  metric are emitted on every output so the agent stays
  observable in the same panel as the systems it watches.

## Getting Started

1. **[Installation]({{< relref "/docs/installation" >}})** — Install the agent on Windows or Linux
2. **[Configuration]({{< relref "/docs/configuration" >}})** — Configure probes, storage, licensing, and updates
3. **[CLI Reference]({{< relref "/docs/cli" >}})** — All available commands
4. **[Web Interface]({{< relref "/docs/web-interface" >}})** — Dashboard, API Explorer and monitoring integration

## Output / Integration

The agent exposes data through four sinks; you enable the ones you
need in the `storage` block of the YAML.

5. **[HTTP / HTTPS endpoint]({{< relref "/docs/http-https" >}})** — REST API, TLS, authentication
6. **[Prometheus / VictoriaMetrics]({{< relref "/docs/prometheus" >}})** — `/metrics` scrape endpoint, OTel-aligned naming, scrape examples
7. **[OTLP / OpenTelemetry]({{< relref "/docs/otlp" >}})** — Native OTLP/gRPC push of metrics + logs (collectors, vmagent, Grafana Cloud)

## Probes

8. **[All Probes]({{< relref "/docs/probes" >}})** — Browse probes by family (Systems, Synthetic, Application Delivery, Virtualization, Hardware, Data Protection, Logs & Events)

## Troubleshooting

9. **[Troubleshooting]({{< relref "/docs/troubleshooting" >}})** — Diagnose and resolve common issues

## Support

- **Email**: support@senhub.io
- **Website**: [senhub.io](https://senhub.io)
