---
title: Configuration — multi-file layout, substitution, versioning
paths:
  - internal/agent/services/configuration/**
---

## Two layouts, one loader

The configuration system supports two layouts; detection is automatic.

### Legacy monolithic (still supported)

A single `agent-config.yaml` with top-level `probes:` and/or `storage:`. The original format. No operator action needed for existing installs.

### Multi-file (Sprint A, introduced 0.1.93)

```
/etc/senhub/                       # Windows: %PROGRAMDATA%\SenHub
├── agent.yaml                     # Global only: agent, cache, auto_update, log
├── probes.d/
│   ├── 01-system.yaml             # YAML array of probe configs
│   └── 10-citrix.yaml
└── strategies.d/
    ├── 01-http.yaml               # Single top-level key = strategy name
    └── 10-otlp.yaml
```

- Files within each `.d/` directory load in **alphabetical** order.
- Files matching `.*` or `*.disabled` are skipped.
- Empty `.d/` directories are valid (zero entries).
- One file per strategy in `strategies.d/` (one top-level key per file).
- Duplicate strategy across files: later file wins, WARN logged.

## Auto-detection rule

`LoadFromDisk` reads `agent.yaml` first. If it contains a top-level `probes:` or `storage:` block, the legacy path is taken and `.d/` directories are **IGNORED** with a one-time WARN log. The operator migrates at their pace by trimming the monolithic file and distributing entries across the `.d/` directories.

## ${env:} / ${file:} substitution

Applies to string VALUES only — never to YAML keys.

| Syntax | Behaviour |
|---|---|
| `${env:VAR}` | env value, or empty string if unset (POSIX shell parity, no error) |
| `${env:VAR:-default}` | env value, or `default` |
| `${file:/path}` | file contents trimmed of whitespace; **error if missing** |
| `${file:/path:-default}` | file contents, or `default` if file missing |
| `$$` | literal `$` (NUL-byte sentinel pre-pass) |

Substitution runs **after** the multi-file merge — a reference in a `strategies.d/` fragment sees the same environment as a reference in the monolithic file.

## config_version bumps

The schema version is in `config_version:` at the top of `agent.yaml`. Current version: **2**.

- Bump only for **breaking** schema changes.
- When bumping, add a migrator entry in `config_migrator.go` so existing installs auto-upgrade.
- Add a documented entry in `docs/admin-guide/CONFIG-VERSION-CHANGELOG.md`.

Non-breaking additions (new optional fields, new probe types, new strategy params) do NOT require a bump.

## yaml.v2 vs yaml.v3

- **`loader.go` uses yaml.v2** to stay consistent with `localConfiguration_manager.go::fixYAMLTypes` (which exists because yaml.v2 returns `map[interface{}]interface{}` for nested maps).
- **`show.go` (output) uses yaml.v3** for deterministic struct marshaling (yaml.v3 emits struct fields in declaration order; yaml.v2 doesn't).
- **Don't mix versions in a single struct without thinking.** When you add a new field that's a nested map, run the integration tests with `make test` to confirm the conversion path still works.

## Watcher

`localConfiguration_watcher.go` uses fsnotify on `agent-config.yaml` only. **The `.d/` directories are NOT watched** — operator must restart the agent (or trigger a config reload event) after editing fragments. Extending the watcher to `.d/` is a future sprint.

## CLI

The agent exposes three config CLI verbs:

- `config check [path]` — validates the configuration (errors + warnings count).
- `config show [--raw|--resolved|--redact] [path]` — prints merged config as sorted YAML.
- (planned) `config migrate` — explicit migration trigger.

When you add a config field, also update `checkConfig` (`cmd/agent/config.go`) so `config check` validates it.

## Secrets in config

- For secrets, prefer `${file:/etc/senhub/secrets/<name>}` references over inline values — `agent config show --redact` masks them, file permissions limit blast radius, and rotation doesn't require an agent restart if combined with the watcher (future).
- Inline secrets must never be logged. `LoadForShow` + `--redact` is the safe export path.
- `agent.license` field carries a JWT — sensitive but operationally legitimate to log mask-only.
