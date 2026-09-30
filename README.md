# SenHub Agent

A single-binary infrastructure monitoring agent: it collects metrics, logs
and **infrastructure topology** from hosts, applications and network
devices, and serves or pushes them to the monitoring stack you already run:
PRTG, Nagios, Zabbix, Prometheus or any OpenTelemetry backend.

**Website: [agent.senhub.io](https://agent.senhub.io)** · Documentation:
[agent.senhub.io/docs](https://agent.senhub.io/docs)

[![Go tests](https://github.com/senhub-io/senhub-agent/actions/workflows/go-test.yml/badge.svg)](https://github.com/senhub-io/senhub-agent/actions/workflows/go-test.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## What it does

- **Hosts** (free): CPU, memory, network, disks and filesystems, processes,
  OS updates, SMART, IPMI, GPUs, time sync, OS logs (systemd journal,
  Windows Event Log), file tailing.
- **Applications and databases** (free): MySQL, PostgreSQL, SQL Server,
  Oracle, MongoDB, Redis, Elasticsearch/OpenSearch, Cassandra, ClickHouse,
  Kafka, RabbitMQ, ActiveMQ, NATS, Pulsar, Nginx, Apache, HAProxy, Tomcat,
  WildFly, Docker, Kubernetes, Proxmox, Ceph and more.
- **Active checks** (free): ping, HTTP(S) with certificate expiry, TCP
  connect, DNS resolution. A failing target is a measurement, never a
  probe failure.
- **Network devices** (free): SNMP v2c/v3 polling (MIB-II, IF-MIB, custom
  OIDs), SNMP trap receiver with your MIBs, LLDP and routing topology.
- **Universal collection** (free): embedded OTLP receiver, Prometheus
  scraping, syslog receiver, and an exec probe that runs your Nagios
  plugins unchanged.
- **Topology**: OpenTelemetry entity events (hosts, services, devices,
  interfaces, routes) with their relationships, sharing identity keys with
  the metrics and logs. [Toise](https://github.com/toise-dev/toise)
  consumes them to build an infrastructure graph you can query at any
  point in time.
- **Paid probes** (Pro/Enterprise licence): Citrix, NetScaler, Veeam,
  Redfish, IBM i, Dell PowerStore, SQL Server and Oracle high availability,
  vSphere and Hyper-V HA, Active Directory hybrid, Exchange Online, Azure
  Container Apps, synthetic web checks.

## Outputs

- **PRTG** and **Nagios**: pull endpoints for their HTTP sensors and checks.
- **Zabbix**: native active agent with generated templates and
  autoregistration (`senhub-agent zabbix setup`), Zabbix 6.0 to 8.0.
- **Prometheus**: scrape endpoint.
- **OTLP**: push over gRPC or HTTP, for metrics, logs and entity events.
- A built-in **web console** to configure and check the agent.

Every metric follows the OpenTelemetry semantic conventions internally;
the format of each output is derived from it.

## Install

**Linux**: download the ZIP for your platform from the
[releases page](https://github.com/senhub-io/senhub-agent/releases)
(`senhub-agent-linux-<arch>.zip`; the `-oss-` variants carry the free
probes only), then:

```bash
unzip senhub-agent-linux-amd64.zip
sudo ./senhub-agent install     # registers the service and writes a default configuration
sudo ./senhub-agent start
```

**Windows**: run the signed MSI from the same page
(`senhub-agent-<version>-amd64.msi`). The installer takes the Zabbix
server and the HTTP port as properties.

**Container**: `ghcr.io/senhub-io/senhub-agent:<version>` (or
`senhub-agent-oss`), configured from environment variables. See
[Running the agent in a container](docs/user-guide/docs/container.md).

The agent runs from local YAML configuration: no account or SaaS
required. Open the console with:

```bash
sudo senhub-agent console          # or --print for its address
```

## Configure

Configuration lives in `agent.yaml`, `probes.d/*.yaml` and
`strategies.d/*.yaml` (a single-file legacy layout is detected). Check a
change with:

```bash
senhub-agent config check
senhub-agent config show --redact
```

Documentation: [agent.senhub.io/docs](https://agent.senhub.io/docs), or
in this repository:

- [Installation](docs/user-guide/docs/installation.md)
- [Configuration](docs/user-guide/docs/configuration.md)
- [Zabbix](docs/user-guide/docs/zabbix.md) ·
  [Prometheus](docs/user-guide/docs/prometheus/index.md) ·
  [OTLP](docs/user-guide/docs/otlp.md) ·
  [Nagios](docs/user-guide/docs/nagios.md)
- [CLI reference](docs/user-guide/docs/cli.md)
- [What's new](docs/user-guide/docs/whats-new/index.md)

## Build from source

```bash
make build          # all platforms
make test           # unit tests; the Makefile is the supported entry point
```

Developer documentation: [docs/developer-guide](docs/developer-guide/README.md)
(architecture, probe authoring, OpenTelemetry conventions).

## License

[Apache 2.0](LICENSE). The free probes (hosts, applications and databases,
checks, SNMP, log and OTLP collection) need no licence key; the paid
probes are unlocked by a SenHub licence.
