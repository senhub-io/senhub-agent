---
title: Follow-ups — open a GitHub issue every time
---

## The rule

Whenever a follow-up is detected during a session — something real that
won't be addressed before the session ends — you **MUST** open a
GitHub issue against `senhub-io/senhubagent` to track it. No follow-up
left in commit messages, release notes, code comments or chat without
an issue number you can quote.

Applies to every session on this repo. The cost of opening an issue is
~30 seconds; the cost of a forgotten follow-up surfacing months later
in production is hours.

## What counts as a follow-up

Anything that would belong in a "Known follow-ups" section if you
wrote one right now. Concretely:

- A `TODO(...)` / `FIXME(...)` you (or anyone) added to the code in
  this session.
- A "deferred to a later PR" decision noted in a commit message or
  release note (the v0.2.0 PR had three of these — they all needed
  issues and got none).
- A bug or drift discovered while doing something else (e.g. during
  OTLP/HTTP work you noticed `swap_*` metrics aren't OTel-mapped —
  that's a follow-up).
- A code-review finding tagged as **Optional** or **Minor** that you
  chose not to fix this round.
- A pre-existing test flake you accepted (e.g. the four documented
  flakes in `internal/agent/services/configuration` under `-race`).
- A doc gap, a config-schema drift, a missing test for a new branch.
- Any "Known follow-ups" / "TODO" bullet you write into a release
  note — each bullet needs an issue number alongside it.

## What does **not** need an issue

- Something you fully fixed before the session ended.
- Transient implementation detail of the current PR (build cache
  workaround, temp variable name, etc.) — these belong in a commit
  message, not an issue.
- Something already tracked: an issue with the same intent already
  open. The search-first step below catches these.

## Procedure

### 1. Search first (dedup)

Before creating, check the repo doesn't already track it:

```bash
gh issue list --repo senhub-io/senhubagent --state open --search "<short keywords>" --limit 10
```

If a matching issue exists, **add a comment** with the new context
instead of opening a duplicate:

```bash
gh issue comment <number> --repo senhub-io/senhubagent --body "<note>"
```

### 2. Create the issue

```bash
gh issue create \
  --repo senhub-io/senhubagent \
  --title "<area>: <one-line specifics>" \
  --label <one-of: bug|enhancement|documentation|question> \
  --body "<see body template below>"
```

Title format: `<area>: <specifics>` — same shape as commit subjects.
Examples:

- `otel-mapping(memory): swap_* metrics have no OTel definition`
- `auto-update: pre-0.2.0 → 0.2.0 transition requires manual binary replace`
- `test(periodic_scheduler): real race remains under -race ./...`

Body template:

```markdown
## Context
<where this came from — session/PR/file:line — what you were doing
when you noticed it>

## What
<the concrete thing to address>

## Why it's deferred
<why this PR didn't fix it — scope, risk, separate concern>

## Acceptance
<how we'll know it's done — a passing test, a doc update, a metric
threshold, a deleted comment>

## Refs
<links to relevant commits, PRs, code lines, prior issues>
```

### 3. Anchor the issue number in the deferring artifact

The whole point of an issue is that the deferred work surfaces later
through GitHub, not through "I should remember". So:

- If the follow-up lives in a `TODO(...)` code comment, append the
  issue number: `// TODO(#234): rename RemoteConfigurationData`.
- If it's a bullet under "Known follow-ups" in a release note, the
  bullet must end with `(#234)`.
- If it's a code-review item that landed in a commit message, mention
  the issue number alongside.

A `TODO` without an issue number is technical debt with no exit;
GitHub issues are the exit.

## When to do it during a session

- **As soon as the follow-up is identified**, not at session end.
  Detecting and deferring are the same moment; the issue creation
  belongs there too.
- **Before the commit that defers the work** — so the commit message
  can quote the issue number.
- **Before writing the "Known follow-ups" section** of a release
  note — every bullet needs an issue number when it's written.

If a session ends with follow-ups you didn't issue, the next session
will inherit untracked debt — and Claude's memory of "I noticed X" is
a session-scoped thing that does not survive.

## What if `gh` isn't authenticated

Stop and tell the user. Don't paper over a missing auth with a
mental note — that's exactly the failure mode this rule exists to
prevent.

## Backfill on detection

When this rule itself is loaded mid-session and you realise the
session has already deferred items without issues, backfill them
immediately — same procedure. Don't wait for the next session.
