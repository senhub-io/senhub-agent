# Current Development

Living roadmap for SenHub Agent: what's active, what recently landed, and the
prioritized backlog. Detail lives in the linked GitHub issues; this file is the
map. Per-area contracts are in `.claude/rules/`.

## Active

### Toise entity detection (#185) — Lot 1 landed, Lot 2 next
The agent emits standard **OpenTelemetry entity events** (nodes) so any
entity-aware backend (the SenHub Toise platform) can build a live infra graph.
Vendor-neutral; relations use a neutral `entity.relation.*` extension while the
OTel relationship spec is still Future Work. Design:
`engineering/ENTITY-DETECTION.md`.
- **Lot 1 (merged #194):** `host` + `service.instance` + `runs_on`, off by
  default (`signals.entities.enabled`), with the `otel.entity.interval`
  liveness backstop. Contract frozen with the Toise team over three rounds.
- **Lot 2 (next):** `db` entity (stable `db.instance.id` = source id, not a
  network address) + `monitors` relation + the full state/delete lifecycle
  tracker. Cross-repo: db-identity extraction lives in the enterprise db probes.
- **Lots 4-5:** host network tables → discovered devices; SNMP topology
  (LLDP/FDB/routes), depends on #156.

### SNMP — the wedge to replace PRTG
Native SNMP collection brings legacy network infra into the VM/Toise/Grafana
stack and lets customers decommission PRTG. Drives both the free-tier collector
story and the topology plane of #185.
- **#156** `snmp_poll` — production-grade: v2c/v3, MIB walker, **device
  discovery + profiles** + UPS/printer/sensor coverage + topology MIBs (p1).
- **#161** `snmp_trap` — passive trap receiver (p2).
- **#188** Grafana network dashboard pack, companion to #156 (p2).

### Zabbix native export — paused after Phase 0 audit (#169)
Spec + audit done (`zabbix/AUDIT-Phase0.md`); ~5 days to implement. Resumes
subject to priorities. Not advertised in the user guide yet.

## Tiering model (open-core + platform)
Free = host self-observability (parity vs node_exporter/windows_exporter) +
universal collection (OTLP receiver, SNMP, OTel events). Paid = deep vendor
integrations + the `event` ingestion + active synthetic checks. Full rationale
in the private `docs/audit/TIERING-STRATEGY.md`. Guides the tier of every new
probe (see `.claude/rules/probes.md` license touch-points).

## Recently completed (2026 dev line)
- **OSS / Enterprise split** — public core (free/host probes, OTel mapper,
  config, http/otlp/senhub strategies, license validator, `app` + `probesdk`)
  vs private enterprise (9 paid probes, ibmi, licence minter). Code landed
  (#182 done, #184/#186 merged); remaining = the public visibility flip (#183,
  gated on a GitHub Support GC).
- **Entity detection Lot 1** (#194) — see Active.
- **swap_* OTel mapping** (#190) — memory probe swap metrics mapped to
  `system.paging.*` / `senhub.system.paging.limit`; closed #137.
- **Multi-file config + value substitution** (0.1.93) — `agent.yaml` +
  `probes.d/` + `strategies.d/`; `${env:}` / `${file:}`; `config show` CLI.
- **IBM i probe** — JT400 bridge, 94/94 metrics OTel-mapped, smoke-tested.
- **OTLP backpressure tier 1** — timeout 10s→60s, scheduler recover,
  cardinality cap, store_size + export_duration + dropped self-metrics.
- **Prometheus / OTel pipeline** — OTLP multi-signal, DB OTel-first, Grafana
  catalog (21 dashboards), self-observability metrics.

## Backlog by thread (open issues)

**A — Detection / Toise:** #185 (Lots 2-5), #189 (guard test: every emitted
metric has an OTel mapping).

**B — SNMP / replace-PRTG:** #156 (p1), #161 (p2), #188 (p2).

**C — Free-tier collector probes:** #154 windows_eventlog (p1, OS parity with
linux_logs), #155 filetail (p1, feeds VictoriaLogs), #158 dns_latency,
#159 tcp_dial, #160 wifi enrichment, #173 OTLP receiver mode (p3).

**D — Distribution / `agent.senhub.io`:** #153 signed Windows MSI (p0,
GPO/SCCM/Intune), #163 Linux .deb/.rpm (p2), #191 Docker image + K8s manifests,
#193 docs fixes (Hint shortcode, broken admin-guide links).

**E — Framework / robustness:** #149 global_tags + universal custom_tags (p0),
#165 OTLP backpressure tier 2 (memory limiter + cardinality budget + persistent
queue, p1), #164 mTLS on senhub + http outputs (p2), #168 footprint benchmark
suite (p3).

**F — Outputs:** #169 finalize Zabbix HTTP output (p3), #172 direct
VictoriaMetrics remote_write (p3).

**G — Probes (other):** #170 VMware vCenter connector (p3).

**Bugs / tech-debt:** #139 auto-update pre-0.2.0 transition, #140 + #141 race
flakes under `-race`, #138 rename the `RemoteConfigurationData` family,
#166 dead `OnDataPoints` cleanup on 5 probes.

## Priority lens (what's next)
- **p0:** #153 (signed MSI), #149 (global_tags / custom_tags).
- **p1:** #165 (backpressure tier 2), #156 (snmp_poll), #155 (filetail),
  #154 (windows_eventlog); entity detection Lot 2 (#185).

## Reference (stable subsystems)
License system → `docs/LICENSE-SYSTEM.md`. OTel naming → canonical
`engineering/../otel/senhub-semantic-conventions.md`. HTTP/PRTG/Nagios outputs,
modular logging, JWT licensing, standalone deployment, Universal Configuration
API — all production-ready; see the user/admin guides.

---

Last updated: 2026-06-02
