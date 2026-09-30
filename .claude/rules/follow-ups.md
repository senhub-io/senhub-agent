---
title: Follow-ups — open a private GitHub issue every time
---

## The rule

Whenever a follow-up is detected during a session — something real that
won't be addressed before the session ends — you **MUST** open a
GitHub issue against the **private** tracker
`senhub-io/senhub-agent-enterprise` to track it. No follow-up left in
commit messages, release notes, code comments or chat without an issue
number you can quote.

Applies to every session on this repo. The cost of opening an issue is
~30 seconds; the cost of a forgotten follow-up surfacing months later
in production is hours.

## The public tracker is not a planning tool

`senhub-io/senhub-agent` is public: what goes in a release, the defects
we find ourselves, lab measurements and internal decisions are not
written there. Users reading it would see our work in progress, and an
issue edited later keeps its first version one click away.

- Follow-ups, release planning (milestones), defects found internally,
  recette results: **private tracker only**.
- The public tracker receives the issues **users** open. Answer them
  there; the internal work they lead to is tracked privately.
- It also keeps a few issues of our own, so that the project reads as
  alive: features a user would want to follow (a new probe, an output,
  packaging), written as a public roadmap item. English, neutral, no
  estimate, no audit or recette finding, no customer or host, no paid-tier
  strategy. When a release adds a user-visible feature worth announcing,
  open or close one there; the working detail stays private.
- Nothing that names a customer, a host, an address or an internal
  machine goes in any issue without being anonymised ("a Windows
  recette host").
- A security weakness goes to the private tracker or a private security
  advisory, never a public issue, until the fix has shipped.

## What counts as a follow-up

Anything that would belong in a "Known follow-ups" section if you
wrote one right now. Concretely:

- A `TODO(...)` / `FIXME(...)` you (or anyone) added to the code in
  this session.
- A "deferred to a later PR" decision noted in a commit message.
- A bug or drift discovered while doing something else.
- A code-review finding tagged as **Optional** or **Minor** that you
  chose not to fix this round.
- A pre-existing test flake you accepted.
- A doc gap, a config-schema drift, a missing test for a new branch.

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
gh issue list --repo senhub-io/senhub-agent-enterprise --state open --search "<short keywords>" --limit 10
```

If a matching issue exists, **add a comment** with the new context
instead of opening a duplicate:

```bash
gh issue comment <number> --repo senhub-io/senhub-agent-enterprise --body "<note>"
```

### 2. Create the issue

```bash
gh issue create \
  --repo senhub-io/senhub-agent-enterprise \
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
  private issue number: `// TODO(ent#234): rename RemoteConfigurationData`.
- If it's a code-review item that landed in a commit message, mention
  the issue number alongside (`ent#234`).
- Release notes are public: they describe what shipped, not what is
  planned. No "Known follow-ups" list pointing at internal work.

A `TODO` without an issue number is technical debt with no exit;
GitHub issues are the exit.

## When to do it during a session

- **As soon as the follow-up is identified**, not at session end.
  Detecting and deferring are the same moment; the issue creation
  belongs there too.
- **Before the commit that defers the work** — so the commit message
  can quote the issue number.

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
