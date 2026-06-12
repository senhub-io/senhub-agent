---
title: Tests — required for behavioural changes
paths:
  - "**/*_test.go"
---

## When tests are required

- **New functionality** → ship with new tests in the same commit.
- **Modified behaviour** → update the existing test that pinned the old behaviour. Don't delete a test "because it fails now" — update the assertion to match the new contract and document why in the commit message.
- **Bug fix** → add a regression test that fails on the old code and passes on the new code.

## How to run tests

| Goal | Command |
|---|---|
| Default unit tests | `make test` |
| With race detector | `make test-race` |
| DB integration (mysql/postgresql) | `make test-database` in senhub-agent-enterprise — the database probes live there since the OSS split |
| Single package, quick iteration | still via `make test` — never raw `go test` |

The 4 historical race flakes in `internal/agent/services/configuration` were fixed in #268 (lock-free config reads moved to atomic snapshots; joinable watcher lifecycle) and the package is back in race CI — a -race failure there is a real regression now.

## Test layout conventions

- **Co-locate with code**: `foo.go` ↔ `foo_test.go` in the same package.
- **Use `_internal_test.go` only when** you need to test unexported helpers from `package foo_test` (rare). Default to in-package tests.
- **Integration tests** that require external services (real MySQL, real PG) live under `<probe>/integration_test.go` with a `//go:build integration` tag.

## Fixture strategy

- **Prefer `t.TempDir()`** over checked-in `testdata/` directories. It's faster, parallel-safe, and the test itself reads cleanly.
- Use `testdata/` only when the input is large (multi-KB YAML fixtures, sample API responses) or when the fixture is hand-crafted to exercise a quirky parser.

## Style

- **Table-driven** when there are 3+ cases of the same shape.
- **Descriptive names**: `TestLoadFromDisk_MonolithicLegacy`, `TestSubstitute_FileMissingNoDefault`. The name should read like the spec line that motivated it.
- **One assertion theme per test**: a test fails for one reason. Don't bundle "checks A, B, C, D" — split.
- **`t.Setenv`** over manual `os.Setenv` + cleanup. The framework restores automatically.

## Race-detector cleanliness

Production code must be race-clean. When `make test-race` flags new races caused by your change:

1. Identify whether it's a real race (shared state without sync) or a test-only race (test setup reaching into the SUT).
2. Real race → fix in production code (add mutex, channel, or restructure ownership).
3. Test-only race → restructure the test (don't disable race for it).

## Mocking & fakes

- **No mocking framework** dependency. Hand-roll fakes for what you need; the codebase prefers concrete types.
- For HTTP fakes, `httptest.NewServer` from stdlib is the convention.
- For DB fakes, use the real DB via testcontainers (`make test-database` in senhub-agent-enterprise, where the database probes live). The cost is worth it — mocked DBs hide migration / driver / wire-format bugs.

## Cache, registry, and other global state

Several services (cache, sensor, configuration manager) carry process-global state. When testing them:

- Reset state in `setup` / `t.Cleanup` to avoid bleed across tests.
- Prefer constructing a fresh instance per test rather than relying on package-level singletons.

## Test data ages with the code

When you refactor a metric name, attribute, or probe shape, **walk every test file** referencing it. Stale test files are silent bugs — they compile and pass but assert against contracts that no longer exist. The compiler can't catch every reference (string literals in `t.Errorf` messages don't fail compilation).
