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

## Features

- **Route a probe's logs to a specific output.** A probe that produces
  logs sent them to every output that reads them; the `endpoints`-style
  filtering that governs metrics did not apply. The new per-probe
  `log_strategies` closes that: `log_strategies: ["otlp"]` sends that
  probe's records to OTLP and nowhere else. Omitting it keeps today's
  behaviour, so nothing changes until you configure it. Only outputs
  that can actually consume logs are accepted — `agent config check`
  rejects anything else instead of letting the records go nowhere.

## Fixes

- **A registry URL with a path no longer breaks updates silently.**
  `auto_update.url` is the *base* the agent appends to, and it appends
  two different things — `/releases/…` for the version list,
  `/download/…` for the artifacts. A value that already carried one of
  those paths made every derived URL double it, so the agent found
  nothing, changed nothing, and stayed on its installed version with
  `auto_update.enabled: true` still in the file. The agent now corrects
  every shape anyone writes (`/releases`, `/download`, a pasted
  `releases.json` URL, a trailing slash), on every code path rather than
  only when the configuration is read, and `agent config check` reports
  the value with the one to write instead. The correction is also logged
  once rather than on every collection cycle.

- **The OTLP log queue no longer fills with records that cannot be
  delivered.** When the receiver refuses a batch for what it contains —
  a malformed payload, an attribute type it does not accept — resending
  it gets the same answer. Those records used to be written to the
  on-disk queue anyway, replayed at every restart and every recovery,
  refused again, and written back; they occupied space the queue then
  took from records that a retry *would* have delivered. They are now
  discarded once and counted, and the queue keeps doing what it is for:
  riding out an outage.

- **A dead listener is now reported as unhealthy.** The syslog, event
  and OTLP receiver probes wait for data to arrive rather than polling
  for it, so they had nothing to derive health from and reported healthy
  on every cycle — including cycles where their socket had been closed
  for hours because the port was taken at startup or the server stopped
  with an error on its own goroutine. They now report the state of the
  listener itself, so `senhub-agent status` and
  `senhub_agent_probes_healthy` tell you when one has stopped receiving.

- **A probe can no longer ship untagged data by omission.** `probe_name`
  and `probe_type` are now added centrally to every datapoint that does
  not already carry them. Previously each probe added them itself, and a
  probe that forgot produced series nothing could tell apart — and whose
  cache entries collided with another instance's.

- **An intake outage can no longer grow the event backlog until the
  agent dies.** The cloud metrics and PRTG outputs already capped what
  they hold when a destination is unreachable; the event output did not,
  and every failed send appended the whole batch to a list nothing
  trimmed. It is now bounded like the others: past the cap the oldest
  events go first, and the loss is counted rather than silent.

- **A rejected batch is no longer resent forever.** When the intake
  refuses a batch of events for what it contains — a malformed payload,
  an unprocessable body — resending the same bytes gets the same answer.
  The agent now recognises that class, drops the batch once, and moves
  on, instead of pinning it at the head of the retry backlog where it
  blocked everything queued behind it and burned a round-trip (plus two
  seconds of retry sleep) on every cycle.

- **A failing output is now visible before it starts losing data.**
  Until the backlog reaches its cap an output that cannot deliver sheds
  nothing, so no counter moved and the only trace was a log line. The
  new `senhub.agent.export.send.failed{strategy,reason}` counts failed
  delivery attempts per output, with `reason` saying whether the batch
  was kept (`transport`) or dropped (`validation`, `configuration`).

- **Stopping the agent no longer races itself.** Every service — the
  configuration loader, the outputs, the probe pool, the auto-updater —
  now shares one cancellation, and each gets its own shutdown allowance
  instead of competing for a single five-second budget the first slow
  drain could consume entirely. In practice: an output that takes its
  time flushing its last batch no longer costs the probe pool its chance
  to close connections cleanly. The whole stop stays bounded (twenty
  seconds at worst) so systemd and the Windows service manager never
  escalate to a kill.

- **A configuration reload no longer leaves work behind.** Probes and
  outputs recreated by a reload used to be started without any link to
  the agent's own shutdown, and relied entirely on being stopped
  individually; one that was not left a goroutine running for the life of
  the process. Long-lived agents whose configuration is edited regularly
  were the ones that accumulated them. A start/reload/stop cycle is now
  covered by a test that fails on a single leaked goroutine.

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
