# Current Development

Living roadmap for SenHub Agent: what's active, what recently landed, and the
prioritized backlog. Detail lives in the linked GitHub issues; this file is the
map. Per-area contracts are in `.claude/rules/`.

## State of the line

- **0.2.2 (stable, 2026-06-11)** — the PRTG-replacement funnel: four free
  active checks (icmp/http/tcp/dns), `exec` (Nagios + JSON contracts),
  `prometheus_scrape`, syslog moved to free (17 free probes), the PRTG
  migration guide and the `packs/alerts/` starter packs. Runtime-validated
  on two production agents (Linux + Windows) across VM / VL / Toise before
  promotion. Release notes: `docs/releases/0.2.2.md`.
- **0.2.1 (stable, 2026-06-10)** — entity rail (OTel entity-events, merged
  spec), SNMP/host network topology, five free collectors (snmp_poll,
  snmp_trap, windows_eventlog, filetail, otlp_receiver), native packages,
  open-source flip (public repo, Apache-2.0).

## Active / next

### SNMP production-grade (#156) — the remaining wedge epic
v3 polling, MIB walker, device discovery + profiles, UPS/printer/sensor
coverage. Audit-stated prerequisites first: shared SNMP core (#291),
entity-side scale fixes (#272), per-cycle client/plan rebuild + jitter.
The `discovery:` config block is merged but inert (loud startup warning
since #353).

### Wave 2 — 0.2.3 security & resilience (milestone 4, 21 issues)
Audit-driven: listener hardening, signed auto-update, retry/backoff fixes,
checkpoint zombie lifecycle (#308, confirmed again during the 0.2.2
recette), icmp_check privileged fallback (#357).

### Wave 3 — 0.3.0 foundation (milestone 5, 16 issues)
Golden files (#296) FIRST, then the float32 bus and transformer-map
synchronization work.

### Zabbix native export (#169) — paused after Phase 0 audit
Spec + audit done (`zabbix/AUDIT-Phase0.md`); ~5 days to implement. Resumes
subject to priorities. Not advertised in the user guide.

## Tiering model (open-core + platform)
Free = host self-observability + **active checks** + universal collection
(OTLP receiver, Prometheus scraping, SNMP poll/trap, syslog, filetail,
windows_eventlog, exec) — 17 probes. Paid = deep vendor integrations
(IBM i, databases, Citrix, NetScaler, Veeam, Redfish) + `event` ingestion +
the synthetic webapp suite. Full rationale in the private
`docs/audit/TIERING-STRATEGY.md`; tier touch-points in
`.claude/rules/probes.md`.

## Recently completed (2026 dev line)
- **0.2.2 feature train** — see above; plus repo-hygiene CI (#343), full
  3-OS matrix on every PR (#334), Prometheus annotation-unit fixes (#344),
  reference-table backfill (#345).
- **0.2.1 train** — entity rail + topology, five free collectors, OSS split
  + public flip, .deb/.rpm + signed MSI, global_tags/custom_tags, JWT-only
  licenses, OTLP endpoint failover, logs durable queue.

## Backlog by thread (open issues)

**A — SNMP / wedge:** #156 (epic, prerequisites #291/#272), #188 (Grafana
network dashboard pack), #303 (paid vendor device packs), #306 (flows
collector, paid).

**B — Entity / Toise:** #212 (SNMP topology lots), #239 (edge attrs dropped
by the bare-keys fallback), #240 (agent tenant field for the prod ingress).

**C — Naming / mapping debt:** #348 (doubled `per_second` suffixes on the
OTLP chain), #207 (snmp_poll OTel resolution via snmpmib).

**D — Security / resilience (0.2.3 wave):** #308 (checkpoint zombies),
#357 (icmp ping_group_range), #223 (run-as-non-root), listener hardening
series — see milestone 4.

**E — Foundation (0.3.0 wave):** #296 (golden files first), float32 bus,
transformer-map sync — see milestone 5.

## Priority lens (what's next)
- **p0:** 0.2.3 wave kickoff — #291 (shared SNMP core, unblocks #156).
- **p1:** #156 lots, #308, #348.

## Reference (stable subsystems)
License system → `docs/LICENSE-SYSTEM.md`. OTel naming →
`otel/senhub-semantic-conventions.md` (canonical). Prometheus exposition
names → user-guide metrics reference. HTTP/PRTG/Nagios outputs, modular
logging, JWT licensing, multi-file config — production-ready; see the
user/admin guides.

---

Last updated: 2026-06-11
