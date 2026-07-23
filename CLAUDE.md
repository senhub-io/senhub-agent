# SenHub Agent — Project Index

Infrastructure monitoring agent (Go, ~72k LOC). Single binary, ships to PRTG / Nagios / Prometheus / OTLP / SenHub cloud. Universal Go/git/build/commit conventions live in `~/.claude/CLAUDE.md`; project-specific contracts live in `.claude/rules/` and load contextually by path.

## Where things are

- **Per-area rules (path-scoped)** → `.claude/rules/` (auto-loaded when files match)
- **Full developer documentation** → [`docs/developer-guide/README.md`](./docs/developer-guide/README.md)
- **User documentation** → `docs/user-guide/`
- **Admin / operations** → `docs/admin-guide/`
- **OTel semantic conventions (canonical)** → `docs/developer-guide/otel/senhub-semantic-conventions.md`
- **Release notes** → `docs/releases/`

## ⚠️ Temporary dependency fork

`github.com/citrix/adc-nitro-go` is replaced by `github.com/senhub-io/adc-nitro-go` (singleton stats panic fix, upstream PR #36 pending). Detailed rationale lives in the private companion repo `senhub-io/senhub-internal-docs` (`TEMPORARY-FORK-citrix-adc-nitro-go.md`). Quarterly review; revert when upstream merges.

## Project-specific build conventions

- **Beta tag format**: `X.Y.Z-beta` — **no `v` prefix** (matches the `dev-beta-release.yml` workflow trigger `*.*.*-beta`).
- **Production tag format**: `X.Y.Z`.
- Release tags live on the ENTERPRISE repo (senhub-agent-enterprise); its workflows build and publish. Tagging in THIS repo does not release anything (the OSS mirror tags are pushed by the enterprise workflow). The old `make bump-version` target was removed (#283).
- Distributed binaries matrix: 3 platforms — linux amd64 / linux arm64 / windows amd64 (plus zipped variants). macOS/darwin is a **local-only** dev/test target, never built or published by CI (no customer runs the agent on darwin; the macos runner was a 10x-billing cost sink).

## Release workflow

ALWAYS use the `release-manager` agent + PR merge to `master`. Direct push to `master` does NOT trigger `master-release.yml`. See memory `feedback_release_workflow.md`.

## Architecture in one diagram

```
Probes (internal/agent/probes/*)
   │
   ▼
DataStore (internal/agent/services/data_store/)
   │  ├── buffer.go (cloud batching)
   │  ├── otelmapper/ (neutral OTel mapper, shared by Prom + OTLP)
   │  └── transformers/ (per-probe YAML, format v3)
   ▼
Strategies (internal/agent/services/data_store/strategies/)
   ├── senhub/     → cloud push (intake.senhub.io)
   ├── otlp/       → OTLP gRPC push
   ├── http/       → pull formats (Prometheus, Nagios, PRTG, Zabbix, Web UI)
   ├── prtg/       → PRTG cache format converter
   └── event/      → syslog / winevents flows
```

OTel-first design principle: every internal metric name follows OTel semantic conventions; sink-specific formats are derived in the mapper, not in probe code. Full principle in memory `feedback_otel_first.md`.

## Configuration

Two layouts supported, auto-detected:

- **Legacy monolithic** — single `agent-config.yaml` with `probes:` + `storage:` (existing installs).
- **Multi-file** — `agent.yaml` + `probes.d/*.yaml` + `strategies.d/*.yaml` (added 0.1.93). Full details in `.claude/rules/configuration.md`.

Value substitution: `${env:VAR}`, `${env:VAR:-default}`, `${file:/path}`, `${file:/path:-default}`, `$$` → literal `$`.

`config show` CLI: `agent config show [--raw|--resolved|--redact]`.

## Current development

Active areas:

- Sprint A — Multi-file config + env/file substitution (branch `feat/conf-multifile-envsubst`).
- Zabbix output integration — starting 2026-05-17 (HTTP sub-format).
- Prometheus integration — Phase 2 in progress on `feat/prometheus-otel-mapping`.

See `docs/developer-guide/current-development.md` for the live roadmap.

## License system

Tiers: **Free** (the universal collection tier — OS/host, logs, network checks, and the application/database/broker probes; everything except the paid set), **Pro** (16 deep vendor / HA / cloud / active-check probes: `citrix`, `netscaler`, `veeam`, `redfish`, `ibmi`, `powerstore`, `mssql_ha`, `oracle_enterprise`, `hyperv_ha`, `vsphere_ha`, `ad_hybrid`, `exchange_online`, `event`, `ping_gateway`, `ping_webapp`, `load_webapp`), **Enterprise** (wildcard). The authoritative split is `freeTierProbes` / `paidProbes` in `internal/agent/services/license/` (`license.go` + `probe_catalog.go`).
Full reference: `docs/LICENSE-SYSTEM.md`.

## Where to look for what

| Task | Rule file (path-scoped) |
|---|---|
| Writing or editing a probe | `.claude/rules/probes.md` |
| Adding a strategy / sink | `.claude/rules/output-{cloud,otlp,http}.md` |
| Touching the data store / mapper / transformers | `.claude/rules/data-store.md` |
| Configuration loader, substitution, schema bump | `.claude/rules/configuration.md` |
| Writing tests | `.claude/rules/tests.md` |
| Editing documentation | `.claude/rules/docs.md` |

Rules under `.claude/rules/` auto-load when their `paths:` glob matches the files you're touching.
