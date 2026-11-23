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

The overlay may change only a limited set of settings. In v1 it can enable or disable a local probe, set its interval from 10 to 3600 seconds, set the log level, and switch entities. In v2 it can also define whole managed probes from the server, adopt a local probe, deliver sealed secrets to managed probes, and pause the agent's own auto-update.

The local file wins. A probe disabled locally stays disabled. Every change is verified before use: an ed25519 signature by a pinned key, the tenant, the agent, and a strictly increasing revision. A verified change is applied by a reload. It is confirmed when the probes start; otherwise it is reverted to the previous overlay. An empty overlay brings the agent back to its local configuration. The local files on disk are never modified.

## Managed probes (v2)

The agent publishes its catalogue of probe types and parameters. The server can only create types in that catalogue. `exec` is never in the catalogue unless the operator lists it in `opamp.overlay.managed_probe_types`. When that list is set, only its types can be managed. Managed outputs are not supported in this version.

## Auto-update pause (v2)

`auto_update: paused` from the server suspends the agent's hourly update check. Withdrawing the overlay, disabling OpAMP, or stopping the service resumes it. The pause is kept across a restart while the stored overlay verifies.

## Adopting a local probe (v2)

Adoption lets the server take over a probe that is already defined in your local file. The server sends a managed probe with the same name and type as the local one, marked as adopting it. From then on the server's definition runs and the local copy is inert: the agent does not start it. The local file is not modified. Withdrawing the overlay (an empty overlay) or disabling OpAMP brings the local probe back as it was written.

The agent announces the capability `io.senhub.adopt/v1`. With it, the agent also reports its local probes to the server in the effective configuration, so the server can offer them for adoption. The report lists, for each probe, its name, type, enabled state, interval, parameters (masked like the rest of the effective configuration), the parameters that point at a target (host, URL, port and similar), and the parameters that carry a local secret. It is capped at 200 probes; beyond that the report is cut and marked as truncated, and the probes left out cannot be adopted.

An adoption must keep the probe's name and type. It is refused in these cases:

- the probe is defined by environment variables (`adopt_env_defined`);
- the probe is disabled in the local file (`target_disabled_locally`);
- the adoption changes the target of the probe while the local probe still holds a secret that the new definition does not replace (`adopt_retarget_with_local_secret`). Without this rule, the local password would be sent to a new target.

An adopting probe sets its interval with the `interval` field, not as a parameter.

On restart, the stored overlay is verified again like any other overlay, and the agent checks again that the local probe still allows the adoption. If it no longer does (for example the probe was disabled, renamed or changed type in the file), the adoption is not applied, the agent logs `OpAMP adoption not applied: the local probe no longer allows it`, and the local probe runs as written.

## Sealed secrets (v2)

A managed probe often needs a password or a token. The server never sends it in clear: it seals it for your agent.

1. On first start with OpAMP enabled, the agent generates its own key pair: an X25519 key to open sealed values and an Ed25519 key to sign its rotations. The private keys never leave the host. Only the public keys and a proof of possession are sent to the server.
2. The server seals each secret value for the agent's public key and ships it in the signed overlay. A sealed value opens only on this agent, for this tenant, in this revision, and only for the probe and parameter it was sealed for.
3. The agent opens the value and stores it in its secret store under a name reserved for the fleet, `fleet.<name>`. A name has 1 to 64 characters: lowercase letters, digits, `.`, `_` and `-`, starting with a letter or a digit.
4. The managed probe refers to the value as `${secret:fleet.<name>}`. This is the reference the agent resolves when it starts the probe, and the one you see in the applied overlay and in `config show`. The server writes it as `{"secret": "<name>"}` in the overlay; the agent turns it into the reference above. Only a parameter that the catalogue marks as a secret may carry a reference, and only a `fleet.` name is resolved: a managed probe can never read your own secrets, files or environment variables.

The value never appears in clear in the agent logs, in `config show --redact`, or in the effective configuration reported to the server. A refused overlay leaves the secret store as it was.

The agent announces the capability `io.senhub.secrets/v1` once it holds its key pair. If no secret backend is writable on the host, it logs `OpAMP sealed secrets unavailable: no writable secret backend`, does not announce the capability, and an overlay that carries a sealed value, a secret reference or a rotation request is refused as `schema`. See [Secret Store](secret-store.md) for the backends.

If a secret cannot be opened, the whole overlay is refused as `secret_unsealable`. It happens when:

- a referenced secret has no sealed value, or a sealed value is used for another probe or parameter than the one it was sealed for;
- the value was sealed for a key the agent does not hold (a reinstalled agent, or a key the agent has already retired);
- a name is outside the alphabet above;
- the value is damaged or sealed for another tenant, agent or revision;
- the secret store refuses the write.

## Key rotation

The agent changes its key pair by itself:

- automatically every 90 days, counted from the creation of the current pair;
- when the server asks for it in a revision.

You can also ask for a rotation on the host, for example after a suspected leak:

```bash
senhub-agent opamp rotate-keys
```

The command writes a request in the state directory. The running agent takes it within the hour and publishes the new public keys. It prints `Key rotation requested; the agent rotates within the hour and publishes the new keys to Fleet.`

Only one rotation runs at a time. While a rotation waits for the server, a new request (from the period, the server or the command) is logged and ignored. A server request applies once per revision: replaying the same revision after a restart does not rotate again.

There is no service gap. After a rotation, the agent keeps the previous private key next to the new one and opens sealed values made for either. It deletes the previous key only when the server acknowledges the new one, by sending a revision that names the new key as the one the values are sealed for. A restart before that keeps both keys.

An agent that is reinstalled and has lost its keys generates a new pair that the server cannot link to the old one. The server does not trust it: it keeps sealing nothing for the new key, raises an alert, and waits for an administrator to approve the new key. Compare the fingerprint below with the one the server shows before approving.

## Showing the agent key

```bash
senhub-agent opamp key
```

Normal case:

```text
key_id:      <key id>
fingerprint: <64 hex characters>
pending:     no
```

After a rotation the server has not acknowledged yet:

```text
key_id:      <new key id>
fingerprint: <new fingerprint>
pending:     yes, rotation waiting for Fleet
previous_key_id:      <old key id>
previous_fingerprint: <old fingerprint>
```

Before the agent has created a key, the command prints `no OpAMP key yet` and exits with code 1. It also exits with 1 when the state directory is unknown or the key file is unreadable.

The fingerprint is the lowercase hex SHA-256 of the X25519 public key followed by the Ed25519 public key: 64 characters. To re-approve a key that changed without proof, compare this value with the key the server shows as waiting for approval, and approve only if they are identical.

The agent also logs the key at each start and after a rotation, at INFO level: `OpAMP agent key`, with the fields `key_id` and `fingerprint`.

## State and persistence

The applied overlay and its replay floor are stored in the agent's state directory: `/var/lib/senhub-agent` on Linux, `%ProgramData%\SenHub` on Windows, or `$SENHUB_STATE_DIR`. An agent run outside its service as a non-root user must set `SENHUB_STATE_DIR` to a writable directory, or the overlay is not persisted.

The key pair, with the previous key while a rotation waits for the server, is kept in the agent's secret store, never in the state directory. The state directory holds only `opamp-key.json`, which carries public information (key id and fingerprint) and is what `senhub-agent opamp key` reads, and the rotation request written by `opamp rotate-keys`. An agent that loses its secret store generates a new key pair, which an administrator must approve again on the server.

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
| `target_disabled_locally:<probe>` | The overlay targets, or adopts, a probe disabled in the local file. |
| `duplicate_target` | The overlay targets the same probe twice. |
| `bounds:<op>` | A value is outside the allowed bounds. |
| `op_not_allowed:<op>` | The operation is not allowed in this overlay version. |
| `not_licensed:<probe>` | The change enables a probe the licence does not cover. |
| `revision (applied N)` | The revision is not above the last one applied (N), in v1 or v2. |
| `adopt_target_missing:<probe>` | The overlay adopts a probe that does not exist in the local file (v2). |
| `adopt_type_mismatch:<probe>` | The adopting probe has another type than the local probe (v2). |
| `adopt_env_defined:<probe>` | The local probe is defined by environment variables and cannot be adopted (v2). |
| `adopt_retarget_with_local_secret:<probe>` | The adoption changes the target of a probe that holds a local secret the overlay does not replace (v2). |
| `name_conflict` | A managed probe has the name of a local probe (v2). |
| `param_invalid` | A managed probe or output has a type absent from the catalogue, an unknown parameter or a parameter of the wrong type (v2). Every managed output is refused this way in this version. |
| `type_not_licensed` | A managed probe has a type the licence does not cover (v2). |
| `destination_not_allowed` | A managed output sends to a host outside the signed `allowed_destinations` (v2). |
| `package_invalid` | The `package` block is malformed (v2). |
| `secret_unsealable` | A sealed value cannot be matched to its reference or opened with the agent's keys (v2). See [Sealed secrets](#sealed-secrets-v2). |

This version does not announce packages to the server, so an envelope that carries a `package` block is refused as `schema` before these checks. The same applies to a `sealed` block or a secret reference when the agent has no key pair yet.
