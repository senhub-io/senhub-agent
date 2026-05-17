# Grafana dashboards for SenHub Agent

Dashboards deployed via Grafana's file-based provisioning. Drop into
the Grafana host's provisioned dashboards directory; the running
Grafana picks them up within ~10 s (default `updateIntervalSeconds`).

## Catalog v1 — 27 dashboards

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

### Vendor pack (14 dashboards — Phase 3 + IBM i)

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
| `ibmi-overview.json`           | SenHub IBM i — Overview              | `senhub-ibmi-overview` |
| `ibmi-jobs.json`               | SenHub IBM i — Jobs                  | `senhub-ibmi-jobs` |
| `ibmi-storage.json`            | SenHub IBM i — Storage               | `senhub-ibmi-storage` |
| `ibmi-database.json`           | SenHub IBM i — Database              | `senhub-ibmi-database` |
| `ibmi-network.json`            | SenHub IBM i — Network               | `senhub-ibmi-network` |
| `ibmi-security.json`           | SenHub IBM i — Security              | `senhub-ibmi-security` |

All vendor dashboards carry "**(awaiting live data)**" in their title
until a customer pilot lights up the corresponding probe. Schema is
validated, queries are cross-checked against
`internal/agent/services/data_store/transformers/definitions/<probe>.yaml`
canonical OTel names, but no production data has yet flowed through
them on sha901. The annotation drops on the first customer go-live.

The IBM i pack covers the 94 metrics shipped in 0.1.93-beta (semconv
§4.14): cpu/memory/asp, jobs (aggregate + per-job top-N) + queues
+ scheduled, ASP/disk/spool/user_storage, tables + index_advisor
+ journals, TCP/netstat/HTTP server, user_profile/sysval/watch.
Queries target `senhub_ibmi_*` Prometheus series and are derived
mechanically from the YAML transformer's OTel block (name + unit +
type) following the Prometheus naming rules in
`internal/agent/services/data_store/strategies/http/prometheus/otel_to_prom.go`
(dot→underscore + unit suffix `s`→`seconds`, `By`→`bytes`, `1`→`ratio`;
counters get `_total`). The dashboards will be validated end-to-end
against **pub400.com** (the public IBM i 7.5 test system) during the
0.1.93-beta test campaign; the "awaiting live data" tag drops on the
first customer pilot.

## Standard layout grammar

Every dashboard follows the same shape (see
`research/REFERENCE-DASHBOARDS.md` §3 for the rationale):

- Top row: 4 stat tiles with the headline KPIs of the audience.
- Second row: 2 chunky timeseries for the same KPIs over time.
- Subsequent rows: per-resource drilldowns.
- Last row when applicable: a logs panel filtered by the same vars.

Time range default `now-1h`, refresh `30s`, tags
`["senhub", "agents", "<audience>"]`, schemaVersion 39.

## Datasources expected

Provisioned on sha901 today (must exist on the target Grafana):

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

See `research/`:

- `REFERENCE-DASHBOARDS.md` — survey of canonical dashboards (Grafana
  Cloud Linux/Windows integrations, Node Exporter Full, Grafana
  Alloy mixin, Citrix/NetScaler/Veeam/Redfish references) and the
  layout grammar adopted.
- `CATALOG-PROPOSAL.md` — dashboard-by-dashboard target structure.
- `IMPLEMENTATION-PLAN.md` — 3-phase execution plan (this catalog
  ships Phase 2; Phase 3 adds the vendor pack: Citrix, NetScaler,
  Veeam, Redfish).
