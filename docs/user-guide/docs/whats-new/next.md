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
- **`refresh-unit` no longer drops directives you added to the unit.** A
  directive added inline to `senhub-agent.service` (an `EnvironmentFile=`
  carrying an ingest token, an `Environment=` proxy setting) was silently
  removed when the unit was refreshed from the packaged template, which
  could leave an output starting without its credentials. Directives the
  template does not manage are now carried over into the refreshed unit,
  under a comment saying where they came from. Hardening directives,
  `ExecStart`, `User=` and `Group=` are still decided by the refresh.
- **A configured output that fails to start is now visible.** Until now
  the only trace was one error line at boot: the agent kept running with
  its remaining outputs, so a broken one could go unnoticed for as long as
  nobody read the journal. A strategy that is configured but not running
  is reported by `senhub-agent status`, and exported as
  `senhub.agent.strategy.failed{strategy,reason}` (no series once every
  output runs). Fixing the configuration clears it on the next reload,
  without restarting the agent.

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
