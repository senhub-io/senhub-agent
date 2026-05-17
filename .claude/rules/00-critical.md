---
title: Critical rules — always-on invariants
---

These rules are non-negotiable and apply to every change in this repository.

## The five NOs

1. **NO automatic push.** Never `git push` without an explicit, fresh approval from the user. A previous "ok" does not authorize a later push.
2. **NO automatic release.** Tags, beta releases, and master merges all require explicit approval. Use the `release-manager` agent for tag flows (cf. memory `feedback_release_workflow.md`).
3. **NO direct commit on `dev` or `master`.** Always work on a feature/fix branch (`feat/...`, `fix/...`, `chore/...`, etc.). `dev` and `master` only receive merges from PRs.
4. **Feature branches only.** Branch from `dev`. Merge to `dev` via PR. Promote to `master` via PR only after explicit approval.
5. **ALWAYS `make test`.** Never run `go test` directly — the Makefile sets up the right environment (locale, log dir, env vars). Same rule for `make build`, `make test-race`, `make test-database`.

## Forks & temporary dependencies

The repo carries one temporary fork that must remain visible:

- **`github.com/citrix/adc-nitro-go`** is replaced by `github.com/senhub-io/adc-nitro-go` (singleton stats panic fix, upstream PR #36 pending). See `docs/.internal/TEMPORARY-FORK-citrix-adc-nitro-go.md`. Quarterly review; revert when upstream merges.

Never silently update or remove a `replace` directive. Always confirm with the user.

## Authorization scope

User approval is scope-limited. "OK pour merger PR #97" does NOT authorize merging #98 the next day. Re-ask for each visible action.

## When in doubt

Pause and ask. Cost of a question: 30 seconds. Cost of an unwanted action: hours of cleanup, possibly published artifacts that can't be retracted.
