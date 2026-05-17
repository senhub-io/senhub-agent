---
title: Build & test — Makefile is the only entry point
---

## Always go through the Makefile

| Want to … | Command |
|---|---|
| Run all unit tests | `make test` |
| Run with race detector | `make test-race` |
| Run DB-integration tests (testcontainers) | `make test-database` |
| Build for current host | `make build-darwin` / `make build-linux` / `make build-windows` |
| Build all platforms | `make build` |
| Create distribution zips | `make package` |
| Run locally without installing | `make run` |
| Watch-rebuild during dev | `make watch` |
| Clean dist + artifacts | `make clean` |

Why: the Makefile sets locale (`LC_ALL=C` matters for some tests), creates the log directory the agent expects, injects version and commit-hash via `-ldflags`, and ensures cross-platform builds use the right CGO settings. `go test` / `go build` skip all of that and produce inconsistent results.

## Cross-platform parity

- Code must build on **darwin / linux / windows** (amd64 + arm64 where applicable).
- `make build-darwin` and `make build-linux` should both pass before opening a PR. Windows is built in CI; if your change is Windows-specific (perfmon, eventlog, service control), test there too.
- Platform-specific code lives in `_<os>.go` files (e.g. `process_metrics_windows.go`, `process_metrics_unix.go`). Don't `runtime.GOOS` switch inside otherwise-portable functions.

## What "green" means

- `make test` → exit 0, no `--- FAIL:` lines.
- `make test-race` is informational on the configuration package (4 pre-existing flakes documented in `feedback_*` memory if it ever comes back). For everything else, race-clean is the standard.
- `make build-darwin && make build-linux` both succeed.
- `go vet ./...` is clean (run automatically by some sub-targets; safe to run manually).
- `govulncheck ./...` reports no vulnerabilities in our own code.

## Versioning

- Current version is in the Makefile (`VERSION := …`). Bump only via the `release-manager` agent.
- Beta tag format: `X.Y.Z-beta` (no `v` prefix, **never**).
- Production tag format: `X.Y.Z`.
- `make bump-version` exists but adds a `v` prefix that breaks the `dev-beta-release.yml` trigger pattern (`*.*.*-beta`). Prefer manual tag creation through the release-manager flow.

## Distributed binaries

The release workflow produces 5 binaries per release: darwin amd64 / darwin arm64 / linux amd64 / linux arm64 / windows amd64 (plus zipped variants). Don't ship a release without the full matrix.
