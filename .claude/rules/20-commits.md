---
title: Git commits — message style and authorship
---

## Message format

Conventional-style first line: `verb(scope): description`.

- **verb**: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`, `revert`.
- **scope**: package or subsystem (`probes`, `mysql`, `otlp`, `config`, `cli`, `http`, `release-manager`, `ci`, `database-probes`, …). Keep it short and lowercase.
- **description**: imperative, present tense, no trailing period. ≤ 72 characters.

Examples:

```
feat(database-probes): OTel-first refactor for mysql + postgresql
fix(sanitize): lift int32 cap on byte counters
chore(0.1.92-beta): pre-release quality pass — fixes + Go/deps bump + docs
```

## Body

- Wrap at ~72 characters per line. One blank line between the subject and the body.
- Explain the **why**, not the what. The diff already shows what changed.
- Reference issues / PRs when they explain context the diff doesn't show.
- For bug fixes: name the failure mode that the fix prevents.

## Authorship — strictly no AI signatures

The agent never adds AI-attribution lines. **DO NOT** include:

- `Co-Authored-By: Claude <...>`
- `🤖 Generated with Claude Code`
- `Co-Authored-By: Claude Opus ... <noreply@anthropic.com>`
- Any other AI/agent footer

Every commit must appear as authored solely by the repository owner.

## Atomicity

- One logical change per commit. Refactor / format-only changes go in their own commit.
- A commit that introduces a feature ships with its tests in the same commit.
- A `make test`-failing commit should never land on `dev`. Squash or rebase locally before merging.

## Heredoc for multiline messages

When committing via `git commit -m`, use a heredoc so the body is preserved as-is and there's no shell-quoting drama:

```bash
git commit -m "$(cat <<'EOF'
feat(probes/mysql): handle MariaDB 10.3 thread accounting quirk

Threads_running on MariaDB includes InnoDB background threads, so the
naive idle = connected - running formula can go negative. Emit the
raw `mysql.threads{kind=...}` series and let downstream compute idle.
EOF
)"
```

## Local-only signing

- Never amend a published commit. Always create a new commit.
- Never `--no-verify` to skip hooks. If a hook fails, fix the underlying issue.
- Never `--no-gpg-sign` unless the user explicitly asks for it.
