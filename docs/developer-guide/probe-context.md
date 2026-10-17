# Probe collection context

`types.Probe.Collect()` takes no context. A collection in progress
therefore cannot be cancelled: a shutdown or a configuration reload
waits for it (or abandons it), and a console preview whose request was
dropped leaves a goroutine running against the target until the probe's
own timeouts fire.

`Probe` is also the contract the private repository implements through
`probesdk/types`, so its method set cannot change. Cancellation is added
as an **optional** interface instead.

## The interface

```go
// internal/agent/probes/types, aliased in probesdk/types
type ContextCollector interface {
    CollectContext(ctx context.Context) ([]datapoint.DataPoint, error)
}
```

`ProbePoller` detects it by type assertion on every cycle:

- implemented: the poller calls `CollectContext(ctx)`;
- not implemented: the poller calls `Collect()` exactly as before. Nothing
  changes for such a probe: no deadline, no new error.

A probe that implements it keeps `Collect()` and makes it delegate:

```go
func (p *Probe) Collect() ([]datapoint.DataPoint, error) {
    return p.CollectContext(context.Background())
}
```

so a caller that only knows `Probe` (tests, older tooling, the private
repository) keeps working.

## What the context carries

| Element | Source |
|---|---|
| Cancellation | The scheduler's run context: cancelled when the agent stops, and by `ProbePoller.Shutdown`, which a reload runs on every probe it replaces. |
| Deadline | `max(2 × interval, 5 min)` from the start of the cycle. |
| Trace span | The `probe.collect` span, so client spans a probe opens are its children. |

The deadline is a backstop against a collection that hangs, not the
operational limit. Probes keep their own `timeout` parameters, which
stay the limit an operator tunes. The backstop is deliberately longer
than any timeout a probe applies today: the per-request defaults are 5 to
10 s, the console preview budget is at most 8 s, and a cycle
that outlives two intervals has already lost its next tick. A probe whose
cycle legitimately runs past five minutes and two intervals is cut where
it used to finish; none of the migrated probes is in that case.

When the deadline expires, the error the probe returns wraps
`context.DeadlineExceeded`, which `collectErrorReason` already counts as
`timeout`.

## Previews

The console's "test this configuration" path (`collectProbeOnce`) passes
the HTTP request's context, bounded by the test budget, to
`CollectContext`. A client that closes the page or the connection
cancels the collection; a probe without `CollectContext` behaves as
before (the HTTP side stops waiting, the collection finishes on its own).

## What a migrated probe does with it

- Every outbound call takes `ctx`: `http.NewRequestWithContext`,
  `QueryContext` / `QueryRowContext` / `PingContext`, client-go calls,
  `net.Dialer.DialContext`.
- Per-request timeouts stay: derive them with `context.WithTimeout(ctx,
  p.cfg.Timeout)` rather than from `context.Background()`.
- On cancellation the probe returns promptly with an error that wraps
  `ctx.Err()` (`fmt.Errorf("...: %w", err)`), so `errors.Is(err,
  context.Canceled)` holds.
- Datapoints already gathered may be returned with the error under the
  usual `PartialResultsProbe` rule; nothing else changes.

## Migrated probes

Probes whose collection is network I/O through a client that already
accepts a context: `postgresql`, `mysql`, `redis`, `http_check`,
`tcp_dial`, `kubernetes`, `kubelet_stats`, `elasticsearch`, `opensearch`,
`prometheus_scrape`, `docker`. Each has a test proving a cancelled
context returns promptly with `ctx.Err()` wrapped.
