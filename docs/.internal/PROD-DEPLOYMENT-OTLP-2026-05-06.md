# Production deployment — OTLP push from sha901 + sha501

**Date:** 2026-05-06
**Branch deployed:** `feat/otlp-export` @ commit `29cf932`
**Operator:** Matthieu

First production deployment of the OTel-first push path. Two servers
observed: sha901 (collector host, loopback push) and sha501 (remote
push over public DNS with TLS + bearer auth).

## Topology

```
                              sha901 (51.255.49.247)
   ┌─────────────────────────────────────────────────────────┐
   │ otelcol-contrib :4317 (gRPC, TLS, bearer auth)          │
   │   ├─ metrics  → prometheusremotewrite → VictoriaMetrics │
   │   └─ logs     → otlphttp/logs → VictoriaLogs (:9428)    │
   │                                                         │
   │ senhub-agent.service                                    │
   │   probes: cpu, memory, network, logicaldisk, linux_logs │
   │   push  → 127.0.0.1:4317 (TLS+bearer, self-loop)        │
   │   service.name = sha901-prod                            │
   └─────────────────────────────────────────────────────────┘
                                ▲
                                │ TLS+bearer
                                │ via eu-west-1.intake-dev.senhub.io:4317
                                │
                              sha501 (217.182.253.243)
   ┌─────────────────────────────────────────────────────────┐
   │ senhub-agent.service                                    │
   │   probes: cpu, memory, network, logicaldisk, linux_logs │
   │   service.name = sha501-prod                            │
   └─────────────────────────────────────────────────────────┘
```

## Authentication

- **TLS**: existing Let's Encrypt cert at
  `/etc/letsencrypt/live/eu-west-1.intake-dev.senhub.io/`
  (managed by certbot, auto-renew enabled).
- **Bearer token**: stored in `~/.senhub/secrets.yaml.age`
  under `otlp.sha901.bearer_token`. Same token used by both agents
  and by the collector's `bearertokenauth/server` extension.

## sha901 collector — what changed

Backups before any edit:
- `/etc/otel-collector/config.yaml.bak-otlp-validate-2026-05-06`
  (Phase 4 validation snapshot — pure additive change, OTLP receiver
  routed into metrics+logs pipelines)
- `/etc/otel-collector/config.yaml.bak-otlp-tls-1746546489`
  (pre-TLS+bearer snapshot)
- `/etc/systemd/system/otel-collector.service.bak-otlp-tls`

Active edits in `/etc/otel-collector/config.yaml`:
1. OTLP gRPC receiver bound to `0.0.0.0:4317` (was `127.0.0.1:4317`)
2. TLS block referencing the existing Let's Encrypt cert
3. `auth.authenticator: bearertokenauth/server`
4. New `extensions:` section with `bearertokenauth/server`
5. Service references the extension under `service.extensions`

systemd unit (`/etc/systemd/system/otel-collector.service`):
- `User=root` (was `User=otel-collector`) — required to read the LE
  privkey (mode 600 root) and the bearer.env (mode 600 root).
  Acceptable trade-off for now; future cleanup: certbot deploy hook
  to copy material into a perms-controlled drop dir, then revert to
  `User=otel-collector`.
- `EnvironmentFile=/etc/otel-collector/bearer.env` — provides
  `OTLP_BEARER_TOKEN` to the collector for the
  `${env:OTLP_BEARER_TOKEN}` reference in config.

ufw rule:
- `4317/tcp ALLOW IN from 217.182.253.243` (sha501 IP), comment
  "OTLP from sha501". Only sha501 can reach the public OTLP gRPC
  endpoint; loopback unrestricted.

## sha901 agent

```
/usr/local/bin/senhub-agent                       (mode 755 root)
/etc/senhub-agent/config.yaml                     (mode 600 root) ← contains bearer literal
/etc/systemd/system/senhub-agent.service          (mode 644 root)
```

Agent key: `a5f503ab-2613-4721-8b8d-2a2da3fac285` (free-tier, no license JWT).

Probes: cpu/memory/network/logicaldisk (free) + linux_logs
(free since commit 29cf932).

OTLP push: `127.0.0.1:4317` with `tls.enabled: true,
insecure_skip_verify: true` (loopback bypasses cert SAN check) +
Bearer header.

## sha501 agent

```
/usr/local/bin/senhub-agent                       (mode 755 root)
/etc/senhub-agent/config.yaml                     (mode 600 root) ← contains bearer literal
/etc/systemd/system/senhub-agent.service          (mode 644 root)
```

Agent key: `4e9f913c-248e-4a6b-b081-0e679e56f300`.

OTLP push: `eu-west-1.intake-dev.senhub.io:4317` with `tls.enabled:
true` (full cert validation, hostname matches SAN) + Bearer header.

## Verification queries

```bash
# Metric series count per server (should be > 15)
curl -G http://127.0.0.1:8427/api/v1/query \
  --data-urlencode 'query=count(count by(__name__) ({service_name="sha901-prod"}))'
curl -G http://127.0.0.1:8427/api/v1/query \
  --data-urlencode 'query=count(count by(__name__) ({service_name="sha501-prod"}))'

# Live memory
curl -G http://127.0.0.1:8427/api/v1/query \
  --data-urlencode 'query=system_memory_usage_bytes{service_name=~"sha.*-prod",system_memory_state="used"}'

# Recent logs from each
curl -X POST -G http://127.0.0.1:9428/select/logsql/query \
  --data-urlencode 'query=service.name:"sha901-prod"' --data 'limit=5'
curl -X POST -G http://127.0.0.1:9428/select/logsql/query \
  --data-urlencode 'query=service.name:"sha501-prod"' --data 'limit=5'
```

## Revert procedure

If the OTLP push needs to be disabled:

1. Stop the agent on each server:
   ```
   sudo systemctl stop senhub-agent
   sudo systemctl disable senhub-agent
   ```
2. Restore the collector config:
   ```
   sudo cp /etc/otel-collector/config.yaml.bak-otlp-tls-1746546489 \
           /etc/otel-collector/config.yaml
   sudo cp /etc/systemd/system/otel-collector.service.bak-otlp-tls \
           /etc/systemd/system/otel-collector.service
   sudo systemctl daemon-reload
   sudo systemctl restart otel-collector
   ```
3. Close the firewall rule on sha901:
   ```
   sudo ufw delete allow proto tcp from 217.182.253.243 to any port 4317
   ```
4. Optionally remove the bearer secret from age:
   ```
   ~/.senhub/edit-secrets.sh   # remove the otlp: block
   ```

## 2026-05-11 hardening pass — done

Three loose ends from the initial deployment were closed:

1. **`${env:VAR}` expansion in agent OTLP config** — same syntax as
   the OTel collector. Agent config no longer embeds the bearer in
   plaintext; it references `${env:OTLP_BEARER_TOKEN}` and the value
   is loaded from `/etc/senhub-agent/bearer.env` (mode 600 root) via
   the unit's `EnvironmentFile=`. Config itself dropped to mode 644.

2. **Collector hardened off root** — `otel-collector` user added to
   `ssl-cert` group, certbot deploy hook at
   `/etc/letsencrypt/renewal-hooks/deploy/otel-collector-perms.sh`
   keeps the Let's Encrypt privkey at `640 root:ssl-cert` after every
   renewal and reloads the collector. Unit reverted to
   `User=otel-collector` with `SupplementaryGroups=ssl-cert`.
   `bearer.env` ownership changed to `640 root:otel-collector`.

3. **YAML lint guard** — `go test` in
   `internal/agent/services/data_store/transformers/` walks every
   definitions YAML and fails if a metric exposes an `otel:` block
   without declaring its probe-side `unit:`. Prevents reintroducing
   the OTLP-side scale bug on a new probe.

## Deploying the same pattern on Windows

The agent binary, OTLP config, and `${env:VAR}` expansion are
cross-OS. Only the secret-loading mechanism differs:

- **Linux (systemd)** — `EnvironmentFile=/etc/senhub-agent/bearer.env`
  in the unit. File mode 600 root.
- **Windows (services)** — set the env var on the service itself:

  ```powershell
  # Run once as Administrator
  $svcName = "SenHubAgent"
  $token   = "<paste from age secret store>"
  $existing = (Get-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\$svcName" -Name "Environment" -ErrorAction SilentlyContinue).Environment
  $newEnv = @("OTLP_BEARER_TOKEN=$token")
  if ($existing) { $newEnv = $existing + $newEnv }
  Set-ItemProperty -Path "HKLM:\SYSTEM\CurrentControlSet\Services\$svcName" -Name "Environment" -Value $newEnv -Type MultiString
  Restart-Service $svcName
  ```

  The agent process reads it via `os.Getenv` exactly like on Linux.
  Acceptable trade-off: the token sits in the registry instead of a
  file, but only the LocalSystem (or whichever the service runs as)
  can read it; not visible in `ps`-style listings.

## 2026-05-12 Grafana catalog rollout — done

21 dashboards shipped in the Grafana folder `senhub-agents` on the
sha901 instance:

- **Linux (7)** — Overview / Fleet / CPU & System / Memory /
  Filesystem / Network / Logs — live with sha901-prod + sha501-prod.
- **Windows (5)** — Overview / Fleet / CPU & System / Disks &
  Filesystems / Logs — live with bbcloud-prod (Windows Server 2022).
- **Agent self-monitoring (1)** — live on all three agents,
  surfaces the new senhub.agent.process.* + senhub.agent.otlp.*
  metric families added in the feat/agent-self-observability branch.
- **Vendor pack (8 — Veeam x2, Redfish x2, NetScaler x2, Citrix
  VDI x2)** — schema-validated, queries cross-checked against the
  canonical OTel names in definitions/*.yaml, all titled
  "(awaiting live data)" until a customer pilot lights them up.

bbcloud deployment specifics — Windows service installed via
sc.exe, OTLP_BEARER_TOKEN set in service registry MultiString
(see PowerShell snippet above), ufw rule on sha901 allowing
4317/tcp from 51.77.231.102.

Cross-OS asymmetry resolved: the Windows logicaldisk probe now
emits system_filesystem state="used" alongside state="free" (was
free-only). The dashboards moved off the `1 - free` workaround.
Bytes-used (vs percent-used) on Windows still requires a WMI
source — see follow-ups below.

## Known remaining follow-ups

- **Linux logs cardinality** — `priority: 6` filters out debug, but
  on busy hosts the volume may still warrant a stricter filter
  (`priority: 4` for warnings+errors only) once we measure storage
  growth in VictoriaLogs.

- **Windows logicaldisk: bytes used via WMI** — `disk_used_percent`
  is now emitted, but `system_filesystem_usage_bytes` with
  `state="used"` is NOT. The percent ratio covers most dashboard
  needs; bytes-used requires `Win32_LogicalDisk.Size` via WMI,
  which is heavier than the existing PDH path. Defer until a
  dashboard panel actually needs it.

- **Vendor dashboards `(awaiting live data)` drop** — manual edit
  of the JSON title on first customer pilot of each probe type
  (Citrix / NetScaler / Veeam / Redfish). Document on go-live; the
  schema and queries are already known-correct.

- **Customer pilot for vendor probes** — first live data flow per
  probe type will validate every query and inevitably surface
  small dashboard adjustments. Plan a 1-day per-probe iteration
  window when the first customer ships each one.
