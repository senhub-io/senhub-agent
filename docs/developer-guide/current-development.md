# Current Development

Where the line stands. This page is a pointer, not a plan: release content is
in the release notes, work items are tracked in the private tracker, and
per-area contracts are in `.claude/rules/`. Keeping a roadmap here made it stale
within a release, so it no longer carries one.

## Where to look

| Question | Source |
|---|---|
| Version in development | `VERSION` at the repository root |
| What the next release contains | `docs/user-guide/docs/whats-new/next.md` |
| What shipped, per release | `docs/user-guide/docs/whats-new/` (`docs/releases/` archives up to 0.3.2) |
| Which probes are free or paid | `internal/agent/services/license/probe_catalog.go`, [LICENSE-SYSTEM](../LICENSE-SYSTEM.md) |
| How the process is built | [Architecture](./architecture.md) |
| Configuration contract | `.claude/rules/configuration.md`, [CONFIG-VERSION-CHANGELOG](../admin-guide/CONFIG-VERSION-CHANGELOG.md) |
| Metric and attribute naming | [OTel semantic conventions](./otel/senhub-semantic-conventions.md) |
| Entity model and Toise contract | [ENTITY-DETECTION](./engineering/ENTITY-DETECTION.md), [ENTITY-TELEMETRY-CONTRACT](./engineering/ENTITY-TELEMETRY-CONTRACT.md) |

## Stable subsystems

These are in production use and described in the user and admin guides:

- Outputs: OTLP push, Prometheus exposition, PRTG, Nagios, Zabbix (native
  active agent, TLS and PSK), REST and the web console.
- Multi-file configuration with `${env:}`, `${file:}` and `${secret:}`
  substitution.
- The entity rail (OTLP only today) and SNMP topology, including the
  `discovery:` block of `snmp_poll`.
- JWT licensing, signed auto-update, native packages and the signed MSI.

## Process

Follow-ups found during a session are filed in the private tracker
(`senhub-io/senhub-agent-enterprise`) as soon as they are identified; see
`.claude/rules/follow-ups.md`. Releases go through the release-manager flow and
a merge to `master`, never a direct push.
