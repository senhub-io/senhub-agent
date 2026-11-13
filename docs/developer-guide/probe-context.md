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

The first collection runs inside `Start`, under the scheduler's
lifecycle lock. `Shutdown` cancels the run context before it waits for
that lock, so a stop issued meanwhile interrupts the first collection
as it does every later cycle.

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
`timeout`. A cycle cancelled because the probe is being stopped is not
counted as a collection error and does not change the probe's health:
it says nothing about the target.

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
- An interrupted cycle publishes nothing and records nothing: no up=0,
  no check result, no entity marked unreachable, no error counter, no
  identity pinned from a query that did not finish. A failure while the
  context is still alive is handled exactly as before.

## Migrated probes

Probes whose collection is network I/O through a client that already
accepts a context: `postgresql`, `mysql`, `redis`, `http_check`,
`tcp_dial`, `kubernetes`, `kubelet_stats`, `elasticsearch`, `opensearch`,
`prometheus_scrape`, `docker`, and the network probes `mongodb`,
`rabbitmq`, `kafka`, `nats`, `consul`, `couchdb`, `influxdb`,
`clickhouse`, `activemq`, `wildfly`, `solr`, `haproxy`, `nginx`,
`apache`, `phpfpm`, `jenkins`, `envoy`, `pulsar`, `ceph`, `proxmox`,
`unifi`, `dns_latency`, `ntp`, `icmp_check`, `mssql`, `oracle`,
`cassandra`, `zookeeper`, `memcached`, `varnish`. Each has a test
proving a cancelled context returns promptly with `ctx.Err()` wrapped,
and a compile-time assertion that it still implements
`ContextCollector`.

`redis` speaks RESP over a raw connection; the end of the context
closes that connection, which releases a blocked read or write.
`zookeeper`, `memcached` and `ntp` do the same on their raw TCP or UDP
connection.

`docker` negotiates the Engine API version under the same context; an
interrupted negotiation is not remembered.

Two clients take no context and are run aside instead. `kafka` (sarama)
closes its connections when the context ends, which fails every call in
flight; a dial already underway ends at its own timeout and its result
is closed and discarded. `dns_latency` abandons the lookup, because the
Go resolver does not watch the context while it waits for an answer; the
lookup's own deadline bounds the abandoned goroutine.

Where a probe pins an identity or an entity attribute from a query
(ClickHouse `serverUUID`, Ceph fsid, Solr cluster id, ActiveMQ
`BrokerId`, Cassandra `host_id`, Oracle version, UniFi controller id),
that query failing because the context ended pins nothing: the next
cycle asks again.

`snmp_poll` is not migrated. gosnmp accepts a context, but the
topology sweep that runs inside the cycle treats a walk that failed as a
partial result and retires the entities it did not see, so a cancelled
walk would have to be told apart from a failed one in every walker
before the probe could be cancelled safely.
