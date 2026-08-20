# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Features

- **Push OTLP straight to a backend that serves it under a base path.**
  `endpoint` is a `host:port` pair and cannot carry a path, which ruled
  out backends exposing OTLP under a prefix. The new
  `url_path_prefix` (OTLP/HTTP only) is prepended to the standard signal
  paths, so `/api/v2/otlp` sends to `/api/v2/otlp/v1/metrics` and its
  siblings. Setting it with `protocol: grpc` is refused at config load
  rather than silently ignored. Documented with a Dynatrace example.

- **Close idle OTLP/HTTP connections before the ingress does.** A sparse
  signal (logs, typically) can leave its connection idle long enough for
  a load balancer to close it, and the agent then pays a failed request
  discovering it. The new `idle_conn_timeout`, set below your ingress
  idle timeout, makes the agent close first and reconnect cleanly. Unset
  keeps the previous behaviour (the Go default of 90 seconds), so
  nothing changes until you configure it.

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

- **PRTG push channels are now named like pull channels.** The same
  measurement reached PRTG under two different names depending on how it
  travelled: the agent pushed `cpu_core_usage` while a PRTG pull showed
  `CPU Core 0 Usage`. Both now resolve the display name from the probe's
  definition, so one device has one channel set.

    | Before (push) | After (push and pull) |
    |---|---|
    | `cpu_core_usage` | `CPU Core 0 Usage` |
    | `mem_used_percent` | `Memory Used %` |

    **PRTG treats a renamed channel as a new one**, so a sensor fed by the
    push strategy will start fresh channels and lose the history attached
    to the old names. Plan the upgrade of push-fed sensors accordingly.
    Pull-fed sensors (the HTTP Data Advanced ones) are unaffected: their
    names do not change.

    A probe that sets an explicit `prtg_metric_id` still names its own
    channel — that override is unchanged.
