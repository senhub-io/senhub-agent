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
- **Two operating modes.** *Offline* runs entirely from a local
  YAML. *Online* syncs probe configuration from the SenHub backend.
- **Tiered licensing.** Free OS probes; Pro/Enterprise unlock the
  vendor probes (Citrix, NetScaler, Redfish, Veeam, …).
- **Self-observability built in.** The agent itself emits health,
  resource and push-pipeline metrics on every output.

## Getting Started

1. **[Installation]({{< relref "/docs/installation" >}})** - Install the agent on Windows or Linux
2. **[Configuration]({{< relref "/docs/configuration" >}})** - Configure probes, storage, licensing, and updates
3. **[CLI Reference]({{< relref "/docs/cli" >}})** - All available commands
4. **[Web Interface]({{< relref "/docs/web-interface" >}})** - Dashboard and monitoring system integration

## Probes

5. **[All Probes]({{< relref "/docs/probes" >}})** - System, infrastructure, application, and backup monitoring

## Troubleshooting

6. **[Troubleshooting]({{< relref "/docs/troubleshooting" >}})** - Diagnose and resolve common issues

## Support

- **Email**: support@senhub.io
- **Website**: [senhub.io](https://senhub.io)
