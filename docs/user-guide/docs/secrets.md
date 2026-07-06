# Managing secrets

From version 0.5.0 (configuration version 3) the agent keeps probe and strategy
credentials in an **OS-native secret store** instead of in plaintext inside your
configuration files. Passwords, tokens, SNMP communities, connection strings and
similar fields are sealed into the store and replaced in the config by a
`${secret:<name>}` reference.

This page covers how the store works, how to reference secrets, and the
`agent secret` commands you use to manage them.

## Why a secret store

Configuration files are edited by hand, copied into support tickets, and
sometimes committed to version control. A plaintext password in any of those
places is a leak waiting to happen. The secret store keeps the sensitive value
out of the config file entirely — the file only holds a reference, and the
value lives encrypted at rest, readable only by the agent.

## Referencing a secret

A sealed secret is read back with the `${secret:<name>}` substitution, resolved
against the active secret backend when the agent loads its configuration:

```yaml
- name: "Production Citrix"
  type: citrix
  params:
    base_url: "https://director.company.com"
    auth:
      username: "DOMAIN\\svc-monitoring"
      password: "${secret:production-citrix.auth.password}"
```

The name follows the pattern `<instance>.<field>`:

- `<instance>` is the probe or strategy name (`Production Citrix` becomes
  `production-citrix` once sanitised).
- `<field>` is the dotted path to the field, including nested maps and slice
  indexes — for example `auth.password` or `v3.users.0.auth_password`.

Like `${env:...}` and `${file:...}`, a `${secret:...}` reference can carry a
default: `${secret:name:-fallback}` resolves to `fallback` when the secret is
missing. Without a default, a missing secret **aborts agent boot** with the
name (never the value) in the error message. Substitution applies to string
**values** only, never to YAML keys.

## Auto-seal on install and boot

You do not have to seal secrets by hand. On install and on every boot the agent:

1. **Harmonises the layout** — if it finds a legacy monolithic `agent.yaml`
   (top-level `probes:` / `storage:`), it first converts it to the multi-file
   layout (`agent.yaml` + `probes.d/` + `strategies.d/`).
2. **Scans for inline secrets** — fields whose name denotes a secret
   (`password`, `passphrase`, `token`, `api_key`, `community`, `credential`,
   `dsn`, `uri`, `private_key`) carrying a plaintext value that is not already a
   `${...}` reference.
3. **Backs up each file it is about to rewrite** with `0600` permissions.
4. **Seals** each plaintext value into the OS-native store and rewrites the
   field to a `${secret:<instance>.<field>}` reference.
5. **Verifies** that every resolved reference equals the original plaintext. On
   any mismatch it **restores the backup** and aborts, leaving the config
   untouched.

The step is **idempotent**: a config that already uses `${secret:...}` — or that
has no inline secrets — is left unchanged. Once a secret is sealed the file's
`config_version` becomes `3`.

To run the seal on demand, without waiting for the next boot:

```bash
agent secret migrate
```

## The `agent secret` command

The `secret` verb manages the store. A secret **value is never taken from the
command line** (it would leak through `ps` and shell history) — `set` reads it
from a hidden prompt, from standard input, or from a file.

```bash
agent secret status          # show the active backend and store location
agent secret list            # list secret names (never values)
agent secret set <name>      # store or replace a secret (hidden prompt)
agent secret get <name>      # print a secret value to stdout (deliberate reveal)
agent secret rm <name>       # delete a secret
agent secret migrate         # seal inline plaintext secrets from the config
```

### Storing a secret

Interactive hidden prompt:

```bash
agent secret set production-citrix.auth.password
# Secret value (hidden): ********
```

From a file (no newline is added — a trailing newline is trimmed):

```bash
agent secret set production-citrix.auth.password --from-file /root/citrix.pass
```

From standard input, for scripted provisioning:

```bash
printf '%s' "$CITRIX_PASSWORD" | agent secret set production-citrix.auth.password
```

Then reference it from the probe as
`password: "${secret:production-citrix.auth.password}"`.

### Inspecting the store

```bash
agent secret status
# backend: age-keyfile
# store:   /etc/senhub-agent
# secrets: 3

agent secret list
# production-citrix.auth.password
# netscaler-lb.password
# agent.key
```

`agent secret list` prints only names, never values. Use `agent secret get`
when you deliberately need to read a value back.

### Revealing the agent key

The agent key is the bearer token pollers (PRTG, Nagios, the web UI) use to
reach the agent. It resolves whether the key is still inline or has been sealed
as `${secret:agent.key}`:

```bash
agent key show
```

## Backends per operating system

The active backend depends on the OS. `agent secret status` always shows which
one is in use.

| OS | Backend | Notes |
|---|---|---|
| **All** (default) | `age-keyfile` | File-backed cipher. Secrets are encrypted with an `age` key into `secrets.age`; the key lives in `agent-secret.key` next to the config, readable only by the agent. Works unprivileged from any context, needs no service wiring. |
| **Windows** | `dpapi` | Windows Data Protection API — the OS encrypts secrets to the agent's machine/service context. |
| **Linux (systemd)** | `systemd-creds` | Hardened opt-in. Secrets are stored as systemd encrypted credentials and delivered to the unit at runtime. Selected when the agent runs under a unit wired with `LoadCredentialEncrypted=`, when an existing `creds.d/` store is present, or when forced with `SENHUB_SECRET_BACKEND=systemd-creds`. Otherwise Linux uses `age-keyfile`. |

On Linux, if you run under systemd-creds you may need to regenerate the unit
credential drop-in after changing secrets:

```bash
agent secret wire-unit
```

## Choosing between `${secret:}`, `${file:}` and `${env:}`

All three resolve to a string value; they differ in where the value comes from
and how it is protected.

| Reference | Use it when |
|---|---|
| `${secret:<name>}` | Default for credentials. The value is sealed at rest in the OS-native store and produced automatically by auto-seal. Prefer this for probe and strategy passwords, tokens and communities. |
| `${file:/path}` | The value already lives in a file managed outside the agent — a mounted Kubernetes/Vault secret, a certificate, a license JWT. The agent reads and trims the file at load time. Use `${file:/path:-default}` to tolerate a missing file. |
| `${env:VAR}` | The value is injected into the agent process environment by an orchestrator or systemd unit. An unset variable resolves to an empty string (no error); use `${env:VAR:-default}` for an explicit fallback. |

For most probe credentials, let auto-seal move them into the store and reference
them with `${secret:...}`. Reach for `${file:...}` or `${env:...}` when the
value is owned by an external system that already manages its lifecycle.

## Verifying a configuration with secrets

`agent config show` resolves references so you can confirm the agent sees the
right values, and `--redact` masks them for safe sharing:

```bash
agent config show --resolved   # references substituted (default)
agent config show --raw        # references preserved (audit which file set what)
agent config show --redact     # secrets masked with *** — safe for tickets
```

Never paste the output of `--resolved` into a support ticket; use `--redact`.
