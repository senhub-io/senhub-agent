# probesdk

The public surface of the agent core for code that lives outside this
module. The private `senhub-agent-enterprise` repository builds its probes,
its entity sources and its subcommands against these packages, because Go
forbids importing `senhub-agent.go/internal/...` across module boundaries.
It also imports `senhub-agent.go/app` directly (`app.Main`,
`app.RegisterCommand`, `app.ExtraCommand`).

Most packages are type aliases and thin wrappers over `internal/`. An alias
keeps the internal type's identity, so a change to the internal type is a
change to the SDK, even when no file under `probesdk/` is touched.

## What is stable

Everything exported from a `probesdk` package, including the fields and the
method set of the types it aliases, and the types reachable from them inside
this module. `probesdk/api.txt` is the authoritative list: one line per
name, kind, signature, field and method.

Removing or changing anything in `api.txt` breaks the enterprise build.
Adding to it is safe. Behaviour is covered by the contract tests, not by the
snapshot.

## How a change is reviewed

`TestAPISnapshot` renders the API from the compiler's export data and
compares it to `api.txt`. It fails with a diff as soon as an edit under
`internal/` alters what the SDK exposes.

1. Read the diff. Lines starting with `-` are removals or changes the
   enterprise build will feel; lines starting with `+` are additions.
2. If the change is intended, regenerate with `make probesdk-api`
   (equivalent to `UPDATE_GOLDEN=1 go test ./probesdk/ -run TestAPISnapshot`)
   and commit `api.txt` with the change that caused it.
3. For a removal or a signature change, update the enterprise repository in
   the same release cycle, then tell its maintainers which commit moves the
   surface. The golden diff in the pull request is the notice.

## Contract tests

`fakeprobe_test.go` is a probe written only against `probesdk` (a test checks
that it imports nothing from `internal/`). `contract_test.go` registers it,
lets the agent's own probe poller build, start and stop it, and checks the
datapoints, the entity source, the parameter schema and the export hooks.
`helpers_test.go` covers the thin wrappers directly.
