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

See [Presentation]({{< relref "/docs/presentation" >}}) for the
short product overview — what the agent is for, what it ships, and
what it explicitly does not do.

## Getting Started

1. **[Presentation]({{< relref "/docs/presentation" >}})** — What the agent does, output paths at a glance
2. **[Installation]({{< relref "/docs/installation" >}})** — Install the agent on Windows or Linux
3. **[Configuration]({{< relref "/docs/configuration" >}})** — Configure probes, storage, licensing, and updates
4. **[CLI Reference]({{< relref "/docs/cli" >}})** — All available commands
5. **[Web Interface]({{< relref "/docs/web-interface" >}})** — Dashboard, API Explorer and monitoring integration

## Output / Integration

The agent exposes data through four sinks; you enable the ones you
need in the `storage` block of the YAML.

6. **[HTTP / HTTPS endpoint]({{< relref "/docs/http-https" >}})** — REST API, TLS, authentication
7. **[Prometheus / VictoriaMetrics]({{< relref "/docs/prometheus" >}})** — `/metrics` scrape endpoint, OTel-aligned naming, scrape examples
8. **[OTLP / OpenTelemetry]({{< relref "/docs/otlp" >}})** — Native OTLP/gRPC push of metrics + logs (collectors, vmagent, Grafana Cloud)

## Probes

9. **[All Probes]({{< relref "/docs/probes" >}})** — Browse probes by family (Systems, Synthetic, Application Delivery, Virtualization, Hardware, Data Protection, Logs & Events)

## Troubleshooting

10. **[Troubleshooting]({{< relref "/docs/troubleshooting" >}})** — Diagnose and resolve common issues

## Support

- **Email**: support@senhub.io
- **Website**: [senhub.io](https://senhub.io)
