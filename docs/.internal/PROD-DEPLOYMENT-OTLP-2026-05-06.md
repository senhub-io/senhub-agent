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

## Known follow-ups

- **Env-var expansion in agent OTLP config** — the agent currently
  requires the bearer literal in `/etc/senhub-agent/config.yaml`
  (mode 600 protects it but it's not the OTel collector pattern).
  Implement `${env:VAR}` expansion in the `headers:` parser so the
  agent matches the collector's substitution syntax and can read
  the token from a systemd EnvironmentFile.
- **Collector privkey access** — switch from `User=root` back to
  `User=otel-collector` via a certbot deploy hook that copies the
  material into a path readable by the otel-collector group.
- **Linux logs cardinality** — `priority: 6` filters out debug, but
  on busy hosts the volume may still warrant a stricter filter
  (`priority: 4` for warnings+errors only) once we measure storage
  growth in VictoriaLogs.
