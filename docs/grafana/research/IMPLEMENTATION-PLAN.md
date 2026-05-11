# Grafana catalog — implementation plan

**Companion to:** `REFERENCE-DASHBOARDS.md` (the survey) +
`CATALOG-PROPOSAL.md` (the target catalog).

**Approved decisions:**
- **Phased rollout** — Phase 2 ships host + self-mon first; Phase 3
  ships vendor pack second.
- **Gap-fill first** — three agent-side self-observability gaps
  closed before any dashboard is written. Avoids shipping
  dashboards with empty panels.

**Branches:**
- `feat/agent-self-observability` — branched from `feat/otlp-export`,
  the three gap-fill commits.
- `feat/grafana-catalog-v1` — branched from
  `feat/agent-self-observability`, the host + self-mon dashboards.
- `feat/grafana-catalog-vendors` — branched from `feat/grafana-catalog-v1`,
  the four vendor packs.

Each merges back to `dev` once validated live, in sequence.

---

## Phase 1 — Agent self-observability gap fills

**Branch:** `feat/agent-self-observability`
**Effort:** ~1.5 days
**Deliverable:** three new metric families exposed in the Prometheus
exposition, no behavioral change to existing strategies.

### 1.1 — Process resource metrics

**Goal:** mirror Grafana Alloy's `resources` mixin so the
"SenHub Agent — Self-monitoring" dashboard has real data for the
"Process resources" row.

**Metrics to expose** (all under the existing `senhub_agent_*`
namespace, registered via the Prometheus exposition bridge — no
change to the OTLP self-monitoring policy which deliberately stays
empty to avoid feedback loops):

| Metric | Type | Source |
|---|---|---|
| `senhub_agent_process_cpu_seconds_total` | counter | `runtime/metrics` `/cpu/total:cpu-seconds` or `os.Getrusage` |
| `senhub_agent_process_resident_memory_bytes` | gauge | `runtime/metrics` `/memory/classes/total:bytes` minus released |
| `senhub_agent_process_heap_bytes` | gauge | `runtime/metrics` `/memory/classes/heap/objects:bytes` |
| `senhub_agent_process_goroutines` | gauge | `runtime.NumGoroutine()` |
| `senhub_agent_process_gc_pauses_seconds_total` | counter | `runtime/metrics` `/gc/cycles/total:cycles` mapped to seconds |
| `senhub_agent_process_open_fds` | gauge | Linux: `/proc/self/fd`; Windows: `GetProcessHandleCount` |

**Files:**
- `internal/agent/services/agentstate/process_metrics.go` (new) —
  cross-OS helpers (build-tag `_linux.go`, `_windows.go`, `_other.go`)
- `internal/agent/services/data_store/strategies/http/prometheus/agent_metrics.go` —
  register the new gauges/counters in `BuildAgentRecords`.

**Tests:**
- Unit: ensure each helper returns non-negative values on the build OS.
- Integration: scrape `/metrics` from a test agent, parse with
  `expfmt`, assert all 6 new families present.

**Validation live:** post-deploy on sha901, run
`curl -G http://127.0.0.1:8427/api/v1/query --data-urlencode 'query=senhub_agent_process_resident_memory_bytes{service_name="sha901-prod"}'`
and confirm value > 0.

### 1.2 — OTLP push self-metrics

**Goal:** fulfill the OTLP implementation plan §7 — counters about
the OTLP strategy's own activity. Exposed on Prometheus (not OTLP,
per the plan, to avoid feedback loops).

**Metrics:**

| Metric | Type | Increment site |
|---|---|---|
| `senhub_agent_otlp_metrics_pushed_total` | counter | After `pushOnce` returns success — increment by `count` |
| `senhub_agent_otlp_logs_pushed_total` | counter | Inside `logsPipeline.emit`, increment by 1 per record emitted |
| `senhub_agent_otlp_export_errors_total` | counter | On `pushOnce` error or pipeline shutdown error |
| `senhub_agent_otlp_buffer_fill_ratio` | gauge | Computed at scrape time: `agentstate.LogChannelDepth() / cap` |
| `senhub_agent_otlp_dropped_log_records_total` | counter | Already exists in `agentstate.GetDroppedLogRecordsTotal()`, just expose it |

**Files:**
- `internal/agent/services/data_store/strategies/otlp/strategy.go` —
  increment counters at the appropriate sites (use
  `sync/atomic.Uint64` for thread safety, exposed via accessor
  methods).
- `internal/agent/services/agentstate/log_channel.go` — add
  `LogChannelDepth()` helper for the buffer fill ratio.
- `internal/agent/services/data_store/strategies/http/prometheus/agent_metrics.go` —
  register the 5 new metrics in `BuildAgentRecords`.

**Tests:**
- Unit: assert counters increment in pushMetrics+drain paths
  (extend `metrics_exporter_test.go` and `logs_test.go`).
- E2E: scrape `/metrics`, confirm pushed_total grows monotonically
  while the agent runs.

### 1.3 — `system_processes_count` metric

**Goal:** the Linux + Windows CPU & System dashboards expect a
"running processes" timeseries. Node Exporter exposes
`node_procs_running` / `node_procs_blocked`; we mirror it under
the OTel `system.processes.count` name.

**Metrics:**

| OTel name | Probe-side metric | Source |
|---|---|---|
| `system.processes.count` (gauge, `{process}`) | `processes_total` | Linux: `gopsutil/process.Counts`; Windows: `process.NewProcesses()` |

Add `state=running/blocked/zombie` attribute on Linux (matches
node_exporter); single state on Windows.

**Files:**
- `internal/agent/probes/cpu/cpuProbe_unix.go` /
  `internal/agent/probes/cpu/cpuProbe_windows.go` — emit the new
  datapoint.
- `internal/agent/services/data_store/transformers/definitions/cpu.yaml` —
  YAML entry with `otel:` mapping (unit `{process}`, type `gauge`).

**Tests:**
- `internal/agent/probes/cpu/cpuProbe_test.go` — assert the
  datapoint is in the output of `Collect()`.
- YAML lint test (already in place) verifies `unit:` is declared.

### Phase 1 acceptance

- `make test` green with `-race`
- Cross-OS build (`darwin`, `linux`, `windows`)
- Live on sha901: all new self-metrics scraped and non-zero
- Single commit per metric family + a final commit registering them
  in the agent metrics bridge → 4 commits on the branch
- Merge to `dev` once sha901 reports clean for 1 hour

---

## Phase 2 — Host + self-mon dashboards

**Branch:** `feat/grafana-catalog-v1` (branched from
`feat/agent-self-observability` after merge)
**Effort:** ~2 days
**Deliverable:** 13 dashboard JSON files in `docs/grafana/`, all
deployed to sha901, all validated on real data.

### 2.1 — Linux host (7 dashboards)

**Order of implementation** (most reusable patterns first):

1. **SenHub Linux — Overview** (1 host)
   - Templates the row layout, color scheme, variable names.
   - All other Linux dashboards reuse pieces of this one.
2. **SenHub Linux — Fleet** (multi-host)
   - Heatmap-table pattern, multi-instance.
3. **SenHub Linux — Logs**
   - VictoriaLogs panel with the proven datasource UID +
     queryType=instant + maxLines=200 (from the Phase 4 work).
4. **SenHub Linux — CPU & System**
5. **SenHub Linux — Memory**
6. **SenHub Linux — Filesystem**
7. **SenHub Linux — Network**

Each ships with:
- JSON file `docs/grafana/linux-<view>.json`
- Tagged `["senhub", "agents", "linux"]`
- Deployed to `/var/lib/grafana/dashboards/senhub/`
- Verified live: each panel shows non-empty data for sha901/sha501

### 2.2 — Windows host (5 dashboards)

**Same order, fewer dashboards.** No live data on sha901/sha501
(both are Linux). Verification path:
- Either deploy a third agent on a Windows test VM
- OR mark dashboards "verified for Linux equivalents, Windows panels
  to validate when an agent runs there" — accept this gap until a
  Windows host comes online.

**Decision needed before this step:** spin up a Windows test VM, or
defer? Recommendation: defer — when the first real Windows customer
ships, validate then. Mark dashboards as "Windows v1 — Linux-tested
queries adapted to Windows metric names".

### 2.3 — Agent self-monitoring (1 dashboard)

**SenHub Agent — Self-monitoring**

Built on top of the metrics added in Phase 1. Layout per
`CATALOG-PROPOSAL.md` §3.1.

### Phase 2 acceptance

- 13 JSON files in `docs/grafana/`, schema-valid
- Each deployed and visible on
  `https://eu-west-1.intake-dev.senhub.io/grafana/`
- For Linux + self-mon: every panel shows data
- README in `docs/grafana/` updated with the dashboard list
- One commit per dashboard family (Linux, Windows, self-mon → 3
  commits) + commits for incremental fixes spotted in live testing
- Merge to `dev` after live validation

---

## Phase 3 — Vendor pack

**Branch:** `feat/grafana-catalog-vendors`
**Effort:** ~3 days
**Deliverable:** 8 vendor dashboards.

### Constraint: no live data on sha901/sha501

The host fleet has no Citrix / NetScaler / Veeam / Redfish probes
running today. Three options to validate:

**Option A — Mock data injector** (½ day extra effort):
  - Build a small "test inject" tool that pushes synthetic OTLP
    records matching what the probes would emit.
  - Pros: fully offline, no customer data dependency, deterministic
    test.
  - Cons: another tool to maintain; synthetic data may miss real-
    world quirks.

**Option B — Customer pilot** (variable, weeks):
  - Find a friendly customer running each probe type, ship the
    dashboards in their environment, iterate.
  - Pros: real data, real feedback.
  - Cons: slow, dependent on customer schedules.

**Option C — Schema-only validation** (¼ day extra):
  - Ship the JSON, schema-validate against Grafana, manually
    eyeball-check the queries against the YAML transformer definitions,
    document "verified for schema correctness, awaiting live data".
  - Pros: fast.
  - Cons: dashboards will inevitably need adjustment when real data
    arrives.

**Recommendation:** Option C now (gets the JSON into the catalogue
fast), Option B as we ship to customers. Skip Option A — too much
infra for the value.

### Order of implementation

1. **SenHub Veeam — Jobs** — simplest vendor, well-defined data.
2. **SenHub Veeam — Repositories**
3. **SenHub Redfish — Hardware Health** — exercises the strict-OTel
   `hw.status` expansion pattern, useful template for the others.
4. **SenHub Redfish — Storage & RAID**
5. **SenHub NetScaler — HA & VServers** — richest probe, biggest
   surface.
6. **SenHub NetScaler — Appliance & SSL**
7. **SenHub Citrix VDI — Sessions & Logons** — design from scratch,
   most opinionated.
8. **SenHub Citrix VDI — Capacity & Health**

### Phase 3 acceptance

- 8 JSON files, schema-valid
- Manual query check against the YAML transformer definitions
  documented in commit messages
- Deployed to sha901 Grafana, dashboards listed in the catalog with
  `(awaiting live data)` annotation in the title
- README updated with full 18-dashboard list
- One commit per vendor (Citrix, NetScaler, Veeam, Redfish) → 4 commits
- Merge to `dev`

---

## Cross-cutting concerns

### Naming + organization

All dashboards land in a single Grafana folder `senhub-agents`
provisioned from `/etc/grafana/provisioning/dashboards/senhub-agents.yml`
(NEW — separate from the existing `senhub-intake` folder which
holds the Sensor Factory platform dashboards). Cleaner separation,
operators see "senhub-agents" as a coherent product.

Provisioner YAML:
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

Deployment per dashboard:
```bash
sudo install -m 0644 -o grafana -g grafana \
  docs/grafana/linux-overview.json \
  /var/lib/grafana/dashboards/senhub-agents/linux-overview.json
```

### Datasource UIDs

- `victoriametrics` (Prometheus-compatible)
- `victorialogs` for the older community plugin OR
  `victoriametrics-logs-datasource` (UID `defqbr545b18gf` on this
  Grafana instance, name "VL-SF") — the new official VL plugin,
  which is the one that works in panels.

**Decision**: use `victoriametrics-logs-datasource` UID
`defqbr545b18gf` consistently — it's what we proved works in the
live validation, and it's the plugin Grafana Labs recommends today.

### Style guide

- Time range default: `now-1h`
- Refresh: `30s`
- Color scheme: per
  [`REFERENCE-DASHBOARDS.md` §3.3](REFERENCE-DASHBOARDS.md)
- Templating variables names per §3.2
- Title: `SenHub <audience> — <view>` (em dash, not hyphen)
- UID: `senhub-<audience>-<view>` kebab-case
- Tags: `["senhub", "agents", "<audience>"]`
- Mandatory `schemaVersion: 39` (Grafana v11+ compatible)
- Mandatory `version` field, bumped each iteration

---

## Validation across all phases

Each phase ends with a 1-hour soak on sha901 / sha501:
1. Restart the agent (Phase 1) or refresh Grafana (Phase 2/3)
2. Wait ≥ 5 metric intervals (~2.5 min @ 30s)
3. Open every dashboard, every panel; confirm no "No data" except
   where explicitly expected (Windows panels when no Windows agent,
   vendor panels when no vendor probe)
4. Save screenshots of each dashboard to `docs/grafana/screenshots/`
   for the user-guide doc

## Risks

| Risk | Mitigation |
|---|---|
| Grafana schema version drifts on sha901 (v12.x runs there) | Use `schemaVersion: 39` (well below current — forward compatible) |
| VictoriaLogs LogsQL query syntax changes | Stick to the proven `service.name:~"..."` pattern from Phase 4 validation |
| Vendor probe metrics differ in unexpected ways between probe versions | Lock dashboard queries to the YAML transformer definition names (the canonical source); add a `git-grep` smoke test that fails if a dashboard references a metric not in any `definitions/*.yaml` |
| Operator changes the datasource UID | Document the required UIDs in `docs/grafana/README.md`; offer a one-line `sed` script to rename them if needed |
| Empty panels on first deploy (data not yet arrived) | Always wait the 5-interval soak before validating; mark `(awaiting live data)` in title for known-empty panels |

## Effort summary

| Phase | Work | Days |
|---|---|---|
| 1 — Self-obs gaps | 3 metric families + tests + live verify | 1.5 |
| 2 — Host + self-mon dashboards | 13 dashboards JSON + deploy | 2.0 |
| 3 — Vendor pack | 8 dashboards JSON + schema validation | 3.0 |
| **Total** | **21 dashboards live** | **6.5** |

Plus ~0.5 d for cross-cutting (folder provisioning, README, screenshots).

**Grand total: ~7 days** to ship the full v1 catalog.

## Sign-off checklist

Once all phases ship:

- [ ] `feat/agent-self-observability` merged to `dev`
- [ ] `feat/grafana-catalog-v1` merged to `dev`
- [ ] `feat/grafana-catalog-vendors` merged to `dev`
- [ ] 21 dashboards live on `https://eu-west-1.intake-dev.senhub.io/grafana/dashboards/f/senhub-agents/`
- [ ] `docs/grafana/README.md` lists all 18 dashboards + 3 self-mon metric families
- [ ] Screenshots in `docs/grafana/screenshots/` for the user-guide
- [ ] Release notes 0.1.90-beta describing the catalog
- [ ] Optional: publish the dashboards on `grafana.com` under
      "SenHub" organization (deferred — needs an `org` registration)
