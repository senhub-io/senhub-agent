# Grafana dashboards for SenHub Agent

Dashboards deployed via Grafana's file-based provisioning. Drop into
the Grafana host's provisioned dashboards directory; the running
Grafana picks them up within ~10 s (default `updateIntervalSeconds`).

## Catalog v1 — 21 dashboards

### Linux host (7)

| File | Dashboard | UID |
|---|---|---|
| `linux-overview.json`   | SenHub Linux — Overview     | `senhub-linux-overview` |
| `linux-fleet.json`      | SenHub Linux — Fleet        | `senhub-linux-fleet` |
| `linux-cpu-system.json` | SenHub Linux — CPU & System | `senhub-linux-cpu-system` |
| `linux-memory.json`     | SenHub Linux — Memory       | `senhub-linux-memory` |
| `linux-filesystem.json` | SenHub Linux — Filesystem   | `senhub-linux-filesystem` |
| `linux-network.json`    | SenHub Linux — Network      | `senhub-linux-network` |
| `linux-logs.json`       | SenHub Linux — Logs         | `senhub-linux-logs` |

### Windows host (5)

| File | Dashboard | UID |
|---|---|---|
| `windows-overview.json`          | SenHub Windows — Overview            | `senhub-windows-overview` |
| `windows-fleet.json`             | SenHub Windows — Fleet               | `senhub-windows-fleet` |
| `windows-cpu-system.json`        | SenHub Windows — CPU & System        | `senhub-windows-cpu-system` |
| `windows-disks-filesystems.json` | SenHub Windows — Disks & Filesystems | `senhub-windows-disks-filesystems` |
| `windows-logs.json`              | SenHub Windows — Logs                | `senhub-windows-logs` |

### Agent self-monitoring (1)

| File | Dashboard | UID |
|---|---|---|
| `agent-self-monitoring.json` | SenHub Agent — Self-monitoring | `senhub-agent-self-monitoring` |

### Vendor pack (8 dashboards — Phase 3)

| File | Dashboard | UID |
|---|---|---|
| `veeam-jobs.json`              | SenHub Veeam — Jobs                  | `senhub-veeam-jobs` |
| `veeam-repositories.json`      | SenHub Veeam — Repositories          | `senhub-veeam-repositories` |
| `redfish-hardware-health.json` | SenHub Redfish — Hardware Health     | `senhub-redfish-hardware-health` |
| `redfish-storage-raid.json`    | SenHub Redfish — Storage & RAID      | `senhub-redfish-storage-raid` |
| `netscaler-ha-vservers.json`   | SenHub NetScaler — HA & VServers     | `senhub-netscaler-ha-vservers` |
| `netscaler-appliance-ssl.json` | SenHub NetScaler — Appliance & SSL   | `senhub-netscaler-appliance-ssl` |
| `citrix-sessions-logons.json`  | SenHub Citrix VDI — Sessions & Logons | `senhub-citrix-sessions-logons` |
| `citrix-capacity-health.json`  | SenHub Citrix VDI — Capacity & Health | `senhub-citrix-capacity-health` |

All vendor dashboards carry "**(awaiting live data)**" in their title
until a customer pilot lights up the corresponding probe. Schema is
validated, queries are cross-checked against
`internal/agent/services/data_store/transformers/definitions/<probe>.yaml`
canonical OTel names, but no production data has yet flowed through
them on the operations Grafana host. The annotation drops on the first customer go-live.

## Standard layout grammar

Every dashboard follows the same shape:

- Top row: 4 stat tiles with the headline KPIs of the audience.
- Second row: 2 chunky timeseries for the same KPIs over time.
- Subsequent rows: per-resource drilldowns.
- Last row when applicable: a logs panel filtered by the same vars.

Time range default `now-1h`, refresh `30s`, tags
`["senhub", "agents", "<audience>"]`, schemaVersion 39.

## Data path prerequisite

Every dashboard here queries metrics the agent pushed over OTLP, stored
under their Prometheus-style names, with the resource attributes as
labels. Two things must hold:

- **Names are the OTLP names converted to Prometheus naming**: dots
  become underscores, the unit becomes a suffix, counters end in
  `_total`, and there is no `senhub_` prefix outside the `senhub.*`
  namespace. The queries read `hw_status`, `system_cpu_utilization_ratio`,
  `system_network_io_bytes_total`, `senhub_veeam_job_count`.
- **`service_name` is a label on every series**, promoted from the
  `service.name` resource attribute; the `$service` variable and most
  queries filter on it.

Two paths give that:

- **VictoriaMetrics** ingesting OTLP on `/opentelemetry/v1/metrics`,
  started with `-opentelemetry.usePrometheusNaming`. VictoriaMetrics
  promotes the resource attributes to labels by default, so
  `service.name` becomes `service_name`.
- **An OpenTelemetry Collector** with the `prometheusremotewrite`
  exporter, unit suffixes left on (the default), and
  `resource_to_telemetry_conversion: enabled: true`, so resource
  attributes become labels instead of staying on `target_info`.

The dashboards do not work as they are on:

- **the agent's own Prometheus endpoint** (`/metrics`): it prefixes every
  name with `senhub_` (`senhub_system_cpu_utilization_ratio`,
  `senhub_hw_status`) and carries no `service_name` label; the resource
  sits on `target_info`;
- **a backend that keeps the dotted OTLP names** (`system.cpu.utilization`,
  VictoriaMetrics without `-opentelemetry.usePrometheusNaming`).

## Datasources expected

Provisioned on the operations Grafana host today (must exist on the target Grafana):

- **VictoriaMetrics** — Prometheus-compatible, UID `victoriametrics`,
  URL `http://localhost:8427` (via vmauth)
- **VictoriaMetrics Logs** — UID `defqbr545b18gf` (the
  `victoriametrics-logs-datasource` plugin, name "VL-SF" on this
  Grafana instance)

## Deployment

### Grafana folder

A dedicated Grafana folder `senhub-agents` (UID `senhub-agents`) is
provisioned via
`/etc/grafana/provisioning/dashboards/senhub-agents.yml`:

```yaml
apiVersion: 1
providers:
  - name: senhub-agents
    orgId: 1
    type: file
    folder: senhub-agents
    folderUid: senhub-agents
    options:
      path: /var/lib/grafana/dashboards/senhub-agents
      foldersFromFilesStructure: false
```

### Per-dashboard install

```bash
sudo install -m 0644 -o grafana -g grafana \
  docs/grafana/<file>.json \
  /var/lib/grafana/dashboards/senhub-agents/<file>.json
```

Grafana picks it up within ~10 s. Re-installing the same file
updates the dashboard in place.

### URL once live

`https://eu-west-1.intake-dev.senhub.io/grafana/dashboards/f/senhub-agents/`

## Research artefacts

The Grafana catalog derives from a multi-source survey of canonical
dashboards (Grafana Cloud Linux/Windows integrations, Node Exporter
Full, Grafana Alloy mixin, Citrix/NetScaler/Veeam/Redfish vendor
references). The full survey, the dashboard-by-dashboard catalog
proposal and the phased execution plan live in the SenHub internal
documentation companion repo and are not redistributed alongside
the OSS agent.
