# Next (unreleased)

Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

## Features

- **Close idle OTLP/HTTP connections before the ingress does.** A sparse
  signal (logs, typically) can leave its connection idle long enough for
  a load balancer to close it, and the agent then pays a failed request
  discovering it. The new `idle_conn_timeout`, set below your ingress
  idle timeout, makes the agent close first and reconnect cleanly. Unset
  keeps the previous behaviour (the Go default of 90 seconds), so
  nothing changes until you configure it.
