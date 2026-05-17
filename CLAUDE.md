# SenHub Agent — Project Index

Infrastructure monitoring agent (Go, ~72k LOC). Single binary, ships to PRTG / Nagios / Prometheus / OTLP / SenHub cloud. Source of truth for code conventions, architectural rules and per-area contracts lives in `.claude/rules/`.

## Where things are

- **Code conventions & rules** → `.claude/rules/` (modular, path-scoped)
- **Full developer documentation** → [`docs/developer-guide/README.md`](./docs/developer-guide/README.md)
- **User documentation** → `docs/user-guide/`
- **Admin / operations** → `docs/admin-guide/`
- **OTel semantic conventions (canonical)** → `docs/developer-guide/otel/senhub-semantic-conventions.md`
- **Release notes** → `docs/releases/`

## Critical rules (the five NOs)

Full text in `.claude/rules/00-critical.md`. The reflex layer:

1. **NO automatic push** — every `git push` requires fresh explicit approval.
2. **NO automatic release** — tags, beta releases and master merges go through the `release-manager` agent + explicit approval.
3. **NO direct commit on `dev` or `master`** — feature branch + PR.
4. **Feature branches only** — branch from `dev`, merge via PR.
5. **ALWAYS `make test`** — never `go test` directly. Same for `make build`, `make test-race`, `make test-database`.

## Branch strategy

```bash
git checkout -b feat/my-feature        # branch from dev
# … work + commits …
make test                              # always green before merge
# open PR → dev (and later → master with explicit approval)
```

## Build commands

```bash
make test              # unit tests
make test-race         # race detector
make build-darwin      # macOS
make build-linux       # Linux
make build-windows     # Windows
make build             # all 3 platforms
```

## ⚠️ Temporary dependency fork

`github.com/citrix/adc-nitro-go` is replaced by `github.com/senhub-io/adc-nitro-go` (singleton stats panic fix, upstream PR #36 pending). See `docs/.internal/TEMPORARY-FORK-citrix-adc-nitro-go.md`. Quarterly review; revert when upstream merges.

## Commit conventions (full text: `.claude/rules/20-commits.md`)

`verb(scope): description` — examples: `feat(probes): ...`, `fix(otlp): ...`, `chore(0.1.93-beta): ...`.

**Never include** `Co-Authored-By: Claude` or `Generated with Claude Code` in commit messages.

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

OTel-first design principle: every internal metric name follows OTel semantic conventions; sink-specific formats are derived in the mapper, not in probe code. Memory note `feedback_otel_first.md` carries the full principle.

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

Tiers: **Free** (cpu, memory, logicaldisk, network, linux_logs), **Pro** (most observability probes), **Enterprise** (wildcard).
Full reference: `docs/LICENSE-SYSTEM.md`. License files live in `internal/agent/services/license/`.

## Where to look for what

| Task | Rule file (path-scoped) |
|---|---|
| Writing or editing a probe | `.claude/rules/probes.md` |
| Adding a strategy / sink | `.claude/rules/output-{cloud,otlp,http}.md` |
| Touching the data store / mapper / transformers | `.claude/rules/data-store.md` |
| Configuration loader, substitution, schema bump | `.claude/rules/configuration.md` |
| Writing tests | `.claude/rules/tests.md` |
| Editing documentation | `.claude/rules/docs.md` |

Rules under `.claude/rules/` auto-load when their `paths:` glob matches the files you're touching. The four always-on rules (`00-critical`, `10-go-style`, `20-commits`, `30-build`) load on every session.
