# Secret Store

The agent can resolve `${secret:NAME}` references in its configuration from an OS-native, on-disk secret store, so that passwords, tokens and connection strings never sit in plaintext in `agent.yaml`, `probes.d/*.yaml` or `strategies.d/*.yaml`.

`${secret:NAME}` sits alongside the `${env:}` and `${file:}` substitutions documented in [Configuration → Environment and File Substitution](configuration.md#environment-and-file-substitution). It is resolved at **config load time**, before any probe or strategy starts.

## The `${secret:}` reference

| Syntax | Resolves to |
|---|---|
| `${secret:NAME}` | The value stored under `NAME` in the secret store. Aborts boot if the name is unknown or no backend is available. |
| `${secret:NAME:-fallback}` | The stored value, or `fallback` if the name is unknown. |

A missing required `${secret:}` reference (no default) **aborts agent boot** with the offending name in the error message — a referenced secret that has gone missing must never silently resolve to an empty string. The error carries only the secret NAME, never its value.

The store is only touched when a `${secret:}` reference actually exists: a configuration that uses no `${secret:}` never creates a key file or store on disk.

```yaml
probes:
  - name: veeam-prod
    type: veeam
    params:
      endpoint: "https://veeam.example.com:9419"
      username: monitor
      password: "${secret:veeam-prod.password}"
```

## Backends per OS

The active backend is selected automatically per platform. Each stores its data next to the agent configuration file.

| Platform | Backend | Store files (in the config directory) |
|---|---|---|
| Linux (default) | `age-keyfile` | `agent-secret.key` (X25519 identity, `0600`), `secrets.age` (ciphertext) |
| Linux (systemd, hardened) | `systemd-creds` | `creds.d/*.cred` (one encrypted credential per secret) |
| Windows | `dpapi` | `entropy.bin` (per-install entropy), `secrets.dpapi` (ciphertext, sealed with Windows DPAPI machine-local scope) |
| macOS / other (dev) | `age-keyfile` | `agent-secret.key`, `secrets.age` |

On Linux, the `age-keyfile` backend is the default because it works unprivileged from any context and needs no systemd wiring. The `systemd-creds` backend is the hardened opt-in and is selected when any of the following holds:

- `$CREDENTIALS_DIRECTORY` is set (the daemon runs under a unit that wired `LoadCredentialEncrypted=`);
- `SENHUB_SECRET_BACKEND=systemd-creds` is set (explicit admin opt-in, e.g. for the seal step outside the unit);
- a populated `creds.d/` store already exists.

The store files are owned by the account the agent runs as and readable only by it (age key file `0600`, DPAPI files restricted to SYSTEM + Administrators). Because the store is root-/service-owned, resolving a sealed secret requires the same privilege as the agent itself — see the privilege note under [`agent key show`](#agent-key-show).

## The `agent secret` command

`agent secret` manages the store. A secret VALUE is never accepted on the command line (it would leak via `ps` or shell history); `set` reads it from a hidden prompt, from standard input, or from a file. The command reads and writes the root-/service-owned store, so it runs behind the agent's privilege gate.

| Command | Description |
|---|---|
| `secret set <name>` | Store or replace a secret. The value comes from a hidden prompt (interactive), a line of stdin (piped), or `--from-file <path>`. |
| `secret get <name>` | Print a secret value to stdout — a deliberate reveal. Warns when the output is not a terminal. |
| `secret list` | List secret NAMES only (never values). |
| `secret rm <name>` | Delete a secret. |
| `secret migrate` | Move inline plaintext secrets out of the config into the store and rewrite them as `${secret:}` references (see [Sealing inline secrets](#sealing-inline-secrets-into-the-store)). |
| `secret status` | Show the active backend and store location and the number of stored secrets. |
| `secret wire-unit` | (Linux/systemd-creds) Regenerate the unit credential drop-in. |

All subcommands honour `--config-path <path>` to locate a non-default configuration directory.

```bash
# Store a secret from a hidden prompt
senhub-agent secret set veeam-prod.password
# Secret value (hidden): ********
# stored secret "veeam-prod.password" in age-keyfile; reference it as ${secret:veeam-prod.password}

# Store from a file (no value on the command line)
senhub-agent secret set db.password --from-file /root/db-pass.txt

# Store from stdin (piping)
printf '%s' "$DB_PASS" | senhub-agent secret set db.password

# List names and show backend status
senhub-agent secret list
senhub-agent secret status
```

### `agent key show`

`agent key show` prints the agent key — the bearer token an operator needs to reach the web UI and to configure PRTG/Nagios scrapers. It loads the configuration and prints the **resolved** key, so it works whether the key is still inline or has been sealed into the store as `${secret:agent.key}`.

Resolving a sealed key reads the root-/service-owned store, so `key show` runs behind the same privilege gate as `agent secret` — it is not a read-only command.

```bash
senhub-agent key show
```

## Sealing inline secrets into the store

`agent secret migrate` scans the configuration for fields whose NAME denotes a secret (`password`, `passphrase`, `secret`, `token`, `api_key`, `community`, `credential`, `dsn`, `uri`, `private_key`) and whose value is still an inline plaintext (not already a `${...}` reference). It moves each value into the store and rewrites the field to a `${secret:}` reference. Identifier-style fields such as `user`, `login` and `email` are deliberately left alone.

Before:

```yaml
probes:
  - name: veeam-prod
    type: veeam
    params:
      endpoint: "https://veeam.example.com:9419"
      username: monitor
      password: "S3cr3t!"
```

Run the migration:

```bash
senhub-agent secret migrate
# sealed inline secrets into the store and rewrote them to ${secret:} references
```

After — the plaintext is gone from the config and lives only in the encrypted store:

```yaml
probes:
  - name: veeam-prod
    type: veeam
    params:
      endpoint: "https://veeam.example.com:9419"
      username: monitor
      password: "${secret:veeam-prod.password}"
```

The store key is derived from the owning instance name and the field path (for example `veeam-prod.password`, or `citrix-01.director.auth.password` for nested fields), so every sealed value has a stable, human-readable reference.
