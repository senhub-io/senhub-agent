# Grafana dashboards for SenHub Agent

Dashboards JSON deployed via Grafana's file-based provisioning. Drop
into the Grafana host's provisioned dashboards directory; the running
Grafana picks them up within ~10 s (default
`updateIntervalSeconds`).

## senhub-agents-otlp.json

Overview for agents pushing telemetry via the OTLP strategy. Shows:

- Top stat row: memory %, CPU utilization, load 1m, max filesystem %
- CPU: utilization-by-mode time series + load averages (1m/5m/15m)
- Memory: usage-by-state stacked + utilization ratio
- Network: throughput per interface/direction (rate of
  `system_network_io_bytes_total`) + errors/drops rate
- Filesystem: bargauge per mountpoint + inode utilization
- Logs: VictoriaLogs LogsQL panel filtered on the same servers

Variables:

- `service` — multi-select of `service_name` values matching
  `sha\d{3}-prod` (auto-populated from VM)
- `iface` — multi-select of `network_interface_name` values for the
  current `service` selection

Datasources expected (provisioned on sha901):

- `victoriametrics` — Prometheus-compatible
  (`http://localhost:8427` → vmauth → victoria-metrics)
- `victorialogs` — VictoriaLogs datasource plugin
  (`http://localhost:9428`)

Deployment on sha901:

```
sudo install -m 0644 -o grafana -g grafana \
  docs/grafana/senhub-agents-otlp.json \
  /var/lib/grafana/dashboards/senhub/senhub-agents-otlp.json
```

URL (once provisioning picks it up):
`https://eu-west-1.intake-dev.senhub.io/grafana/d/senhub-agents-otlp/`
