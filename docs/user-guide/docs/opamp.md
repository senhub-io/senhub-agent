# OpAMP

The agent can report itself to an [OpAMP](https://opentelemetry.io/docs/specs/opamp/)
server: what it is, whether it is healthy, and the configuration it runs.
A fleet view built on that server then lists every agent without asking
each one.

The client is **off by default** and **report-only**: the agent describes
itself and accepts nothing from the server. A configuration the server
offers is ignored (the agent logs it once, at `INFO`), and the agent does
not take packages, commands or connection settings from it.

## What is reported

**Identity** (the OpAMP agent description):

| Attribute | Value |
|---|---|
| `service.name` | `senhub-agent` |
| `service.instance.id` | the agent's instance id, the same one its OTLP resource and its entities carry |
| `service.version` | the agent version |
| `host.id`, `host.name`, `host.arch`, `os.type`, `os.description` | the host the agent runs on, the same values as on its OTLP resource and host entity |
| `senhub.agent.edition` | `oss` or `enterprise` |
| `senhub.license.tier` | `free`, `pro` or `enterprise` (`free` when no valid licence is installed) |
| `senhub.agent.config.source` | `local`: the configuration comes from the files on the host |

The OpAMP instance uid is the 16 bytes of `service.instance.id`, so the
server sees the same identity as the telemetry backends.

**Health**, sent when it changes and at every heartbeat:

- the agent as a whole: `running`, `degraded` (a probe or an output is
  failing) or `starting` (a probe has not started yet), with the time the
  agent started;
- one entry per probe, `probe:<name>`: `running`, `failed` (its last cycle
  failed), `retrying` (it could not start and the agent retries it),
  `disabled` (`enabled: false`) or `starting`, with the last error and the
  time it started;
- one entry per output, `output:<name>`: `running`, `failed` (configured
  but not running) or `degraded` (its last delivery failed), with the
  reason.

Error texts are redacted before they leave the host: the password of a
URL, password pairs of a connection string and bearer tokens are masked.

**Effective configuration**: one entry per loaded file (`agent.yaml`,
`probes.d/<file>`, `strategies.d/<file>`; the single file of a monolithic
layout), each in exactly the form `senhub-agent config show --redact`
prints. Probes declared by `SENHUB_PROBE_*` variables come as one more
entry named `environment`. Secrets are masked the way that command masks
them: every value under a key named like a secret, and every value that
came from a `${file:}` or `${secret:}` reference. The configuration is
sent again whenever it changes on disk. It is sent whole, even beyond
1 MiB (the agent then logs one `INFO` line); a server may keep only part
of a larger one.

## What is not reported

No metric, log or trace goes through OpAMP: those keep travelling over
the configured outputs. The agent does not accept remote configuration
yet.

## Configuration

A top-level block of `agent.yaml`:

```yaml
opamp:
  enabled: true
  endpoint: wss://opamp.example.com/v1/opamp
  token: ${secret:opamp.token}
  transport: websocket
  tls:
    ca_file: ""
    insecure_skip_verify: false
  heartbeat_interval: 30s
```

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Turns the client on. |
| `endpoint` | — | Server URL. `wss://` or `ws://` with `transport: websocket`, `https://` or `http://` with `transport: http`. Required when enabled. |
| `token` | — | Bearer token sent as `Authorization: Bearer <token>`. Required when enabled. |
| `transport` | `websocket` | `websocket` keeps one connection open; `http` polls the server. Prefer `websocket`: over `http` the agent cannot tell the server it is stopping, so the server sees it offline only after three missed heartbeats (90 s by default). |
| `tls.ca_file` | — | PEM file of the CA the server certificate is verified against, instead of the system store. |
| `tls.insecure_skip_verify` | `false` | Skips the verification of the server certificate. Also allows a plaintext `ws://` or `http://` endpoint on a remote host. |
| `heartbeat_interval` | `30s` | How often the agent tells the server it is alive when nothing changed. With `transport: http` it is also the polling interval. At least `1s`. |

A change to the block is applied without a restart: turning it on or off,
or changing the endpoint, the token, the transport or the TLS settings,
restarts the client. A new value stored behind a `${secret:}` reference is
read at the next start, as described in [Secret store](secret-store.md).

The client honours `HTTPS_PROXY` and `NO_PROXY` from the service
environment. After a failed connection it retries with an exponential
backoff capped at 5 minutes.

## Security

- **The token is a credential.** Keep it out of the file: store it with
  `senhub-agent secret set` and write `${secret:<name>}`, or point to a
  file with `${file:/etc/senhub-agent/secrets/opamp-token}`. The agent
  never logs it, `config show --redact` masks it, and `config check` does
  not print it.
- **TLS is verified.** A `ws://` or `http://` endpoint sends the token in
  clear, so `config check` refuses it unless the host is a loopback
  address (`localhost`, `127.0.0.1`, `::1`) or `tls.insecure_skip_verify`
  is set explicitly.
- The token identifies the tenant on the server side; the agent sends no
  tenant header.

## Verification

```bash
senhub-agent config check     # validates the block
senhub-agent doctor           # says whether OpAMP is enabled and connected
```

The agent log names the connection state changes: `OpAMP connected`,
`OpAMP connection lost; retrying with backoff`, `OpAMP reconnected`.
Repeated failures while the server stays down are logged at `DEBUG` only.

Self-metrics, present while the client is enabled:

| Metric | Type | Meaning |
|---|---|---|
| `senhub.agent.opamp.connected` | gauge | 1 while the client holds a working connection |
| `senhub.agent.opamp.messages.sent` | counter | status reports (description, health, configuration) handed to the client; heartbeats are not counted |
| `senhub.agent.opamp.errors` | counter, `reason` | `connect`, `server` (the server answered with an error), `report`, `effective_config` |
