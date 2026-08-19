# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Fixes

- **OTLP export failures are now visible per signal.** A failing logs
  pipeline (for example a receiver rejecting every batch) previously
  moved no counter: the failure only surfaced as dead-letter queue
  churn. Failed exports now increment `senhub.agent.otlp.export.errors`
  for every signal, the logs path logs a throttled warning naming the
  error, and `senhub-agent status --otlp`, the web dashboard JSON
  (`/info/otlp`, `export_errors_by_signal`) and the exported metric
  carry the per-signal breakdown.

## Breaking Changes

- **`senhub.agent.otlp.export.errors` gained a `signal` attribute.**
  The unlabeled series is replaced by one series per failing signal;
  the former total is the sum over `signal`. A signal that never failed
  emits no series (counter semantics: absence means zero).

    | Before | After |
    |---|---|
    | `senhub.agent.otlp.export.errors` (no attributes) | `senhub.agent.otlp.export.errors{signal="metrics"\|"logs"\|"traces"}` |

    Migration: replace `senhub_agent_otlp_export_errors_total` in
    dashboards and alert rules by `sum(senhub_agent_otlp_export_errors_total)`
    (any-signal alerting) or pivot on the new `signal` label. The
    `export_errors_total` field of `/info/otlp` is unchanged.
