# OpAMP remote management

## What it does

The OpAMP client lets a fleet server manage one agent remotely. The agent reports its description, health, and effective configuration to the OpAMP server; secrets are masked in that report. With the overlay enabled, the agent can also apply a signed configuration overlay sent by the server. OpAMP requires a licence that grants OpAMP (Enterprise). The client is off by default.

## Configuration

The `opamp:` block of `agent.yaml` configures the client.

| Name | Default | Meaning |
|---|---|---|
| `opamp.enabled` | `false` | Turns the OpAMP client on. |
| `opamp.endpoint` | empty | OpAMP server URL. Required when enabled: `wss://` with websocket, `https://` with http. A plain `ws://` or `http://` endpoint is accepted only on a loopback address or with `tls.insecure_skip_verify: true`. |
| `opamp.token` | empty | Access token. Required when enabled. Use a secret store reference; the token is never written in clear. |
| `opamp.transport` | `websocket` | `websocket` or `http`. |
| `opamp.heartbeat_interval` | `30s` | Heartbeat interval. Minimum `1s`. |
| `opamp.tenant` | empty | Server-side tenant for this agent. Required for the overlay; an overlay naming another tenant is refused. |
| `opamp.tls.ca_file` | empty | Optional CA bundle in PEM form. Without it, the system store is used. |
| `opamp.tls.insecure_skip_verify` | `false` | Skips TLS verification. Not allowed when the overlay is enabled. |
| `opamp.overlay.enabled` | `false` | Accepts a signed remote overlay. Needs a `wss://` or `https://` endpoint, a `tenant` and at least one signing key. |
| `opamp.overlay.signing_keys` | empty | Pinned ed25519 public keys. Each entry has `id` and `public_key`. A key never arrives over OpAMP. |
| `opamp.overlay.managed_probe_types` | empty | Probe types the server may create as managed probes. Empty means every type except `exec`. |

Minimal example:

```yaml
opamp:
  enabled: true
  endpoint: wss://fleet.example.com/v1/opamp
  token: ${secret:fleet.opamp.token}
  tenant: <tenant>
```

Example with the overlay:

```yaml
opamp:
  enabled: true
  endpoint: wss://fleet.example.com/v1/opamp
  token: ${secret:fleet.opamp.token}
  tenant: <tenant>
  overlay:
    enabled: true
    signing_keys: [{id: <id>, public_key: <base64>}]
```

`tls.ca_file` is optional; the system store is used without it. A token is never written in clear; use a secret store reference.

## The signed overlay

The overlay may change only a limited set of settings. In v1 it can enable or disable a local probe, set its interval from 10 to 3600 seconds, set the log level, and switch entities. In v2 it can also define whole managed probes from the server and pause the agent's own auto-update.

The local file wins. A probe disabled locally stays disabled. Every change is verified before use: an ed25519 signature by a pinned key, the tenant, the agent, and a strictly increasing revision. A verified change is applied by a reload. It is confirmed when the probes start; otherwise it is reverted to the previous overlay. An empty overlay brings the agent back to its local configuration. The local files on disk are never modified.

## Managed probes (v2)

The agent publishes its catalogue of probe types and parameters. The server can only create types in that catalogue. `exec` is never in the catalogue unless the operator lists it in `opamp.overlay.managed_probe_types`. When that list is set, only its types can be managed. Managed outputs are not supported in this version.

## Auto-update pause (v2)

`auto_update: paused` from the server suspends the agent's hourly update check. Withdrawing the overlay, disabling OpAMP, or stopping the service resumes it. The pause is kept across a restart while the stored overlay verifies.

## State and persistence

The applied overlay and its replay floor are stored in the agent's state directory: `/var/lib/senhub-agent` on Linux, `%ProgramData%\SenHub` on Windows, or `$SENHUB_STATE_DIR`. An agent run outside its service as a non-root user must set `SENHUB_STATE_DIR` to a writable directory, or the overlay is not persisted.

## Status and troubleshooting

`senhub-agent status` and `senhub-agent doctor` show whether OpAMP is enabled and connected, and the reason when it is not, including an invalid `opamp` block. The first connection failure is logged at WARN; later ones are logged at DEBUG. Remote configuration status values are `APPLYING`, `APPLIED`, and `FAILED` with `rejected: <code>` or `reverted: start_failed:<probe>`.

| Refusal code | Meaning |
|---|---|
| `schema` | The overlay does not match the expected schema. |
| `hash` | The overlay hash does not match its content. |
| `signature` | The ed25519 signature is missing or does not verify, or the envelope's `key_id` differs from the signature's. |
| `unknown_key` | The signature names a key that is not pinned. |
| `tenant` | The overlay names another tenant. |
| `agent` | The overlay names another agent. |
| `target_missing:<probe>` | The overlay targets a probe that does not exist locally. |
| `target_disabled_locally:<probe>` | The overlay targets a probe disabled in the local file. |
| `duplicate_target` | The overlay targets the same probe twice. |
| `bounds:<op>` | A value is outside the allowed bounds. |
| `op_not_allowed:<op>` | The operation is not allowed in this overlay version. |
| `not_licensed:<probe>` | The change enables a probe the licence does not cover. |
| `revision (applied N)` | The revision is not above the last one applied (N), in v1 or v2. |
| `name_conflict` | A managed probe has the name of a local probe (v2). |
| `param_invalid` | A managed probe or output has a type absent from the catalogue, an unknown parameter or a parameter of the wrong type (v2). Every managed output is refused this way in this version. |
| `type_not_licensed` | A managed probe has a type the licence does not cover (v2). |
| `destination_not_allowed` | A managed output sends to a host outside the signed `allowed_destinations` (v2). |
| `package_invalid` | The `package` block is malformed (v2). |
| `secret_unsealable` | A secret reference has no matching sealed value (v2). |

This version announces neither packages nor secrets to the server, so an envelope that carries a `package` block, a `sealed` block or a secret reference is refused as `schema` before these checks.
