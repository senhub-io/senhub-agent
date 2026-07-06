---
title: Configuration — multi-file layout, substitution, versioning
paths:
  - internal/agent/services/configuration/**
---

## Two layouts, one loader

The configuration system supports two layouts; detection is automatic.

### Legacy monolithic (still LOADED — no longer WRITTEN by install)

A single `agent-config.yaml` with top-level `probes:` and/or `storage:`. The original format. **From 0.2.x onward `agent install` writes the multi-file layout, not the monolithic one** — existing monolithic installs keep working transparently. To migrate an existing install: `agent config migrate [path]`.

### Multi-file (default install layout from 0.2.x)

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

## ${env:} / ${file:} / ${secret:} substitution

Applies to string VALUES only — never to YAML keys.

| Syntax | Behaviour |
|---|---|
| `${env:VAR}` | env value, or empty string if unset (POSIX shell parity, no error) |
| `${env:VAR:-default}` | env value, or `default` |
| `${file:/path}` | file contents trimmed of whitespace; **error if missing** |
| `${file:/path:-default}` | file contents, or `default` if file missing |
| `${secret:<name>}` | value from the active OS-native secret backend (`secret.Resolve`); **error if missing** unless a `:-default` is given |
| `$$` | literal `$` (NUL-byte sentinel pre-pass) |

Substitution runs **after** the multi-file merge — a reference in a `strategies.d/` fragment sees the same environment as a reference in the monolithic file.

## config_version bumps

The schema version is in `config_version:` at the top of `agent.yaml`. Current version: **3**.

- Bump only for **breaking** schema changes.
- When bumping, add a migrator entry in `config_migrator.go` so existing installs auto-upgrade.
- Add a documented entry in `docs/admin-guide/CONFIG-VERSION-CHANGELOG.md`.
- v3 (0.5.0+): on install/boot the agent auto-seals inline plaintext secrets into the OS-native store and rewrites them to `${secret:<instance>.<field>}` references (backup 0600 + resolved-value verification + restore on mismatch, idempotent). The bump only happens once a secret is actually sealed; a secret-free v2 config stays v2.

Non-breaking additions (new optional fields, new probe types, new strategy params) do NOT require a bump.

## yaml.v2 vs yaml.v3

- **`loader.go` uses yaml.v2** to stay consistent with `localConfiguration_manager.go::fixYAMLTypes` (which exists because yaml.v2 returns `map[interface{}]interface{}` for nested maps).
- **`show.go` (output) uses yaml.v3** for deterministic struct marshaling (yaml.v3 emits struct fields in declaration order; yaml.v2 doesn't).
- **Don't mix versions in a single struct without thinking.** When you add a new field that's a nested map, run the integration tests with `make test` to confirm the conversion path still works.

## Watcher

`localConfiguration_watcher.go` uses fsnotify on:

- the top-level config file (`configPath` — `agent.yaml` by default), AND
- the sibling `probes.d/` and `strategies.d/` directories when they exist.

Adding, modifying, removing or renaming a fragment file in either `.d/` triggers a reload. Dotfiles and `*.disabled` files are filtered out so editor swap files don't cause spurious reloads.

For a legacy monolithic install (no `.d/` directories on disk), only the top-level file is watched.

## CLI

The agent exposes three config CLI verbs:

- `config check [path]` — validates the configuration (errors + warnings count). Uses `LoadFromDisk` so fragments under `probes.d/` / `strategies.d/` are covered too.
- `config show [--raw|--resolved|--redact] [path]` — prints merged config as sorted YAML.
- `config migrate [path]` — converts a legacy monolithic config into the multi-file layout (timestamped backup before any write, post-write equality check, restores backup on mismatch).

When you add a config field, also update `checkConfig` (`cmd/agent/config.go`) so `config check` validates it.

## Secrets in config

- For secrets, prefer `${file:/etc/senhub/secrets/<name>}` references over inline values — `agent config show --redact` masks them, file permissions limit blast radius, and rotation doesn't require an agent restart if combined with the watcher (future).
- Inline secrets must never be logged. `LoadForShow` + `--redact` is the safe export path.
- `agent.license` field carries a JWT — sensitive but operationally legitimate to log mask-only.
