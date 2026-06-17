# SenHub Agent

A single-binary infrastructure monitoring agent: it collects metrics, logs
and **infrastructure topology** from hosts and network devices, and serves
or pushes them to the monitoring stack you already run.

[![Go tests](https://github.com/senhub-io/senhub-agent/actions/workflows/go-test.yml/badge.svg)](https://github.com/senhub-io/senhub-agent/actions/workflows/go-test.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## What it does

- **Host observability** (free): CPU, memory, network, disks/filesystems,
  OS logs (systemd journal, Windows Event Log), flat-file log tailing,
  Wi-Fi signal.
- **Active checks** (free): multi-target ping, HTTP(S) with TLS-expiry
  tracking, TCP connect, DNS resolution — a failing target is a
  measurement, never a probe failure.
- **Network monitoring** (free): SNMP v2c polling (MIB-II / IF-MIB,
  custom OIDs), SNMP v2c/v3 trap receiver with operator-supplied MIB
  resolution.
- **Universal collection** (free): embedded OTLP receiver, Prometheus
  endpoint scraping, syslog receiver, and an exec probe that runs your
  existing Nagios plugins unchanged.
- **Topology & entities**: emits OpenTelemetry entity events (hosts,
  services, network devices/interfaces/routes) with embedded
  relationships — your metrics, logs and infrastructure graph share the
  same identity keys. [Toise](https://github.com/toise-dev/toise) consumes
  these events to build a temporal infrastructure graph, queryable as of
  any point in time.
- **Outputs**: PRTG and Nagios (pull, primary), Prometheus exposition,
  OTLP push (gRPC/HTTP — metrics, logs and entity events), plus a built-in
  web dashboard.
- **Paid probes** (Pro/Enterprise license): IBM i, MySQL, PostgreSQL,
  Citrix, NetScaler, Veeam, Redfish, and more.

One internal vocabulary, OTel-first: every metric follows OpenTelemetry
semantic conventions internally; sink formats are derived from it.

## Install

Download the ZIP for your platform from the
[releases page](https://github.com/senhub-io/senhub-agent/releases)
(`senhub-agent-<os>-<arch>.zip` — the `-oss-` variants are the free-only
edition), then:

```bash
unzip senhub-agent-linux-amd64.zip
sudo ./senhub-agent install     # registers the service + generates a default config
sudo ./senhub-agent start
```

The agent runs from local YAML configuration only — no account or SaaS
required. Open the dashboard at
`http://localhost:8080/web/{agentkey}/dashboard` (the agent key is printed
at install time and stored in the config).

## Configure

Configuration lives in `agent.yaml` + `probes.d/*.yaml` +
`strategies.d/*.yaml` (a single-file legacy layout is auto-detected).
Validate any change with:

```bash
senhub-agent config check
senhub-agent config show --redact
```

Start here:

- [Installation guide](docs/user-guide/docs/installation.md)
- [Configuration reference](docs/user-guide/docs/configuration.md)
- [OTLP / OpenTelemetry output](docs/user-guide/docs/otlp.md)
- [CLI reference](docs/user-guide/docs/cli.md)

## Build from source

```bash
make build          # all platforms (darwin/linux/windows)
make test           # unit tests — the Makefile is the only supported entry point
```

Developer documentation: [docs/developer-guide](docs/developer-guide/README.md)
— architecture, probe authoring, OTel semantic conventions.

## License

[Apache 2.0](LICENSE). The free tier (host observability, SNMP, OTLP
receiver, log probes) needs no license key; paid probes are unlocked by a
SenHub license.
