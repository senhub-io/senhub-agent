# SenHub agent alerting starter packs

Ready-to-import alert rules for the agent's free-tier probes. They
cover what a PRTG install alerts on out of the box: host thresholds,
availability checks, TLS expiry, SNMP device and interface health,
plus the agent's own delivery pipeline.

| File | Covers |
|---|---|
| `vmalert/host.rules.yml` | CPU, memory, disk capacity and growth, interface errors |
| `vmalert/active-checks.rules.yml` | icmp_check, http_check (incl. TLS expiry under 30/7 days), tcp_dial, dns_latency |
| `vmalert/snmp.rules.yml` | snmp_poll device/interface health, snmp_trap receiver self-metrics |
| `vmalert/agent-health.rules.yml` | probe health, collection errors, OTLP drops and buffer growth |
| `grafana/contact-points.example.yaml` | Grafana contact-point + notification-policy provisioning example |

Metric names are the Prometheus exposition names; both ingestion
paths (Prometheus scrape of the agent, OTLP push through an OTel
collector) produce them. The full name catalog is in the user guide's
metrics reference.

## vmalert

One flag per rule file, or a glob:

```bash
vmalert \
  -datasource.url=http://victoriametrics:8428 \
  -notifier.url=http://alertmanager:9093 \
  -rule="/etc/vmalert/senhub/*.rules.yml"
```

Docker Compose:

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

Validate after editing thresholds:

```bash
vmalert -rule="packs/alerts/vmalert/*.rules.yml" -dryRun
```

## Grafana

Two options:

- **Datasource-managed (recommended with VictoriaMetrics):** run
  vmalert as above; Grafana displays the rules through the
  VictoriaMetrics datasource.
- **Grafana-managed:** import the rule groups via Alerting > Alert
  rules > Import, or provision them with the example in `grafana/`.

## Tuning

Thresholds are deliberately conservative (90% capacity, 3-5 minute
holds on availability). Every rule carries a `severity` label
(`info` / `warning` / `critical`) — route on it in Alertmanager or
Grafana notification policies. Adjust `for:` holds to your scrape
and push intervals: a 60s collection cycle needs at least 2-3
cycles before firing.
