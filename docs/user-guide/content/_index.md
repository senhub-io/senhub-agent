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
or observability tool you already operate (PRTG, Nagios, Prometheus,
OTLP collectors).

![SenHub Agent dashboard](/images/dashboard-hero.webp "SenHub Agent dashboard showing active probes and metrics")

## What it does

- **One collector for many sources.** Each *probe* knows how to
  talk to one class of system. You add probes to the agent's YAML;
  you do not glue together vendor exporters.
- **One vocabulary across every output.** Metric names, units and
  attributes match across PRTG, Nagios, Prometheus and OTLP push.
- **Tiered licensing.** Free OS probes; Pro/Enterprise unlock the
  vendor probes (Citrix, NetScaler, Redfish, Veeam, …).
- **Self-observability built in.** The agent itself emits health,
  resource and push-pipeline metrics on every output.

See the [Presentation]({{< relref "/docs/presentation" >}}) page
for the longer product overview.

## Getting Started

1. **[Presentation]({{< relref "/docs/presentation" >}})** - What the agent does, output paths at a glance
2. **[Installation]({{< relref "/docs/installation" >}})** - Install the agent on Windows or Linux
3. **[Configuration]({{< relref "/docs/configuration" >}})** - Configure probes, storage, licensing, and updates
4. **[CLI Reference]({{< relref "/docs/cli" >}})** - All available commands
5. **[Web Interface]({{< relref "/docs/web-interface" >}})** - Dashboard and monitoring system integration

## Probes

6. **[All Probes]({{< relref "/docs/probes" >}})** - System, infrastructure, application, and backup monitoring

## Troubleshooting

7. **[Troubleshooting]({{< relref "/docs/troubleshooting" >}})** - Diagnose and resolve common issues

## Support

- **Email**: support@senhub.io
- **Website**: [senhub.io](https://senhub.io)
