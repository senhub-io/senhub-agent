# Alert rules and dashboards

The repository ships two optional starter sets: alert rules for vmalert
and Grafana dashboards. Both are samples to adapt, not a product: they
read metrics the agent already produces and need nothing installed on
the agent.

Both live in the repository, next to the agent source:

- Alert rules: [`packs/alerts/`](https://github.com/senhub-io/senhub-agent/tree/master/packs/alerts)
- Dashboards: [`docs/grafana/`](https://github.com/senhub-io/senhub-agent/tree/master/docs/grafana)

## Alert rules

Twenty-five rules in four files, covering what a PRTG installation
alerts on out of the box. Every rule carries a `severity` label
(`info`, `warning` or `critical`) to route on in Alertmanager or in
Grafana notification policies.

| File | Rules |
|---|---|
| `host.rules.yml` | `HostHighCpu` and `HostMemoryPressure` (above 90 percent for 15 minutes), `DiskAlmostFull` (above 90 percent), `DiskWillFillIn24h` (from the 6 hour growth rate), `NetworkInterfaceErrors` (above 10 errors per second) |
| `active-checks.rules.yml` | `IcmpTargetDown`, `IcmpPacketLoss` (above 20 percent), `HttpCheckDown`, `HttpCheckSlow` (above 2 s), `TlsCertExpiringSoon` (under 30 days), `TlsCertExpiryCritical` (under 7 days), `TcpPortDown`, `DnsLookupFailing`, `DnsLookupSlow` (above 500 ms) |
| `snmp.rules.yml` | `SnmpDeviceDown`, `SnmpInterfaceDown` (operationally down while administratively up), `SnmpInterfaceErrors`, `SnmpTrapCommunityMismatch`, `SnmpTrapDecodeFailures` |
| `agent-health.rules.yml` | `AgentProbesUnhealthy`, `AgentCollectErrors`, `AgentOtlpDroppingData`, `AgentOtlpExportErrors`, `AgentOtlpStoreNearCapacity`, `AgentRestarted` |

The thresholds are conservative starting points. Adjust the `for:`
holds to your collection interval: a 60 second cycle needs at least two
or three cycles before an alert should fire.

### Metric names

The expressions use the names of the agent's Prometheus exposition,
which carries the `senhub_` prefix (`senhub_system_cpu_utilization_ratio`,
`senhub_icmp_up`). They match when VictoriaMetrics or Prometheus scrapes
the agent's [`/metrics` endpoint](prometheus/index.md). When the same
metrics arrive by [OTLP push](otlp.md), the standard OpenTelemetry names
lose the prefix in the backend (`system_cpu_utilization_ratio`), so the
host expressions need the prefix removed. Check the name in your backend
with a query before loading a file.

### Load into vmalert

One flag per rule file, or a glob:

```bash
vmalert \
  -datasource.url=http://victoriametrics:8428 \
  -notifier.url=http://alertmanager:9093 \
  -rule="/etc/vmalert/senhub/*.rules.yml"
```

With Docker Compose:

```yaml
services:
  vmalert:
    image: victoriametrics/vmalert
    volumes:
      - ./packs/alerts/vmalert:/etc/vmalert/senhub:ro
    command:
      - -datasource.url=http://victoriametrics:8428
      - -notifier.url=http://alertmanager:9093
      - -rule=/etc/vmalert/senhub/*.rules.yml
```

Validate the files after editing a threshold:

```bash
vmalert -rule="packs/alerts/vmalert/*.rules.yml" -dryRun
```

### Load into Grafana

Two options:

- Datasource-managed, which suits VictoriaMetrics: run vmalert as above
  and Grafana shows the rules through the VictoriaMetrics datasource.
- Grafana-managed: import the rule groups in Alerting, Alert rules,
  Import. `packs/alerts/grafana/contact-points.example.yaml` is a
  provisioning example for the contact point and the notification
  policy (Grafana 10 or later).

## Dashboards

Thirty-three Grafana dashboards, one JSON file each, in `docs/grafana/`.
Every dashboard follows the same layout: four headline tiles, two time
series of the same indicators, then per-resource drill-downs, and a logs
panel where logs exist. The default range is the last hour with a 30
second refresh.

| Area | Dashboards |
|---|---|
| Linux host | Overview, Fleet, CPU and System, Memory, Filesystem, Network, Logs |
| Windows host | Overview, Fleet, CPU and System, Disks and Filesystems, Logs |
| Kubernetes | Workloads, Pod logs |
| Agent | Self-monitoring (the agent's own health) |
| Databases | MySQL and MariaDB overview, PostgreSQL overview |
| Azure | Container Apps, Container App Jobs |
| IBM i | Overview, Database, Jobs, Network, Security, Storage |
| Citrix | Sessions and Logons, Capacity and Health |
| NetScaler | HA and VServers, Appliance and SSL |
| Redfish | Hardware Health, Storage and RAID |
| Veeam | Jobs, Repositories |

The IBM i, Citrix, NetScaler, Redfish and Veeam dashboards carry
"(awaiting live data)" in their title. Their queries are checked against
the metric definitions of each probe, but have not yet been proven on
production data; the label is removed when they are.

### Data path

The dashboards query the names that OTLP data takes in a
Prometheus-compatible backend: dots become underscores, the unit becomes
a suffix, and there is no `senhub_` prefix outside the `senhub.*`
vendor metrics. `service_name` must be a label on every series. Two
paths give that:

- VictoriaMetrics ingesting OTLP on `/opentelemetry/v1/metrics`, started
  with `-opentelemetry.usePrometheusNaming`.
- An OpenTelemetry Collector with the `prometheusremotewrite` exporter
  and `resource_to_telemetry_conversion` enabled.

The dashboards do not work unchanged on a scrape of the agent's own
`/metrics` endpoint, which prefixes every name with `senhub_`. Logs
panels need VictoriaLogs and its Grafana datasource; they were checked
against VictoriaLogs v1.52.0.

### Load into Grafana

Use Grafana file provisioning, or import the JSON from Dashboards, New,
Import:

```bash
sudo install -m 0644 -o grafana -g grafana \
  docs/grafana/linux-overview.json \
  /var/lib/grafana/dashboards/senhub-agents/linux-overview.json
```

Grafana picks up a provisioned file within about ten seconds, and
installing the same file again updates the dashboard in place. Most
dashboards expect a VictoriaMetrics datasource with the UID
`victoriametrics`, which you can change in the JSON; the Kubernetes
dashboards select their datasources through variables.
