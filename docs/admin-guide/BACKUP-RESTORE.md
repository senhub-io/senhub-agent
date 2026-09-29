# Backup and restore

What to copy to rebuild an agent, and the two traps that make a partial
copy look complete.

## What to copy

Everything the agent needs lives in two places: its configuration
directory and its state directory.

| | Linux | Windows |
|---|---|---|
| Configuration | `/etc/senhub-agent/` | `C:\ProgramData\SenHub\` |
| State | `/var/lib/senhub-agent/` | `C:\ProgramData\SenHub\` |

In the configuration directory:

| File | What it holds |
|---|---|
| `agent.yaml` | Global settings, and the agent key or a `${secret:agent.key}` reference to it |
| `probes.d/`, `strategies.d/` | One fragment per probe and per output |
| `nagios.yaml` | The operator's Nagios checks, when there are some |
| `license.jwt` | The licence, when there is one |
| `certs/` | The HTTPS certificate and key, when the console serves HTTPS |
| `agent-secret.key`, `secrets.age` | The sealed secrets (Linux default, `age-keyfile` backend) |
| `creds.d/` | The sealed secrets (Linux, `systemd-creds` backend) |
| `entropy.bin`, `secrets.dpapi` | The sealed secrets (Windows, `dpapi` backend) |

In the state directory: the OTLP checkpoint, the read positions of the
log probes (so a restored agent does not re-read or skip logs), and the
managed binary on hosts upgraded by the agent itself.

Stop the service before copying, or copy twice and compare: the state
directory changes while the agent runs.

## The secret store is part of the configuration

A `${secret:<instance>.<field>}` reference is useless without the store
it points into. Restoring `probes.d/` alone gives an agent that loads,
starts its probes, and cannot authenticate to anything. When the agent
key itself is sealed (`key: "${secret:agent.key}"`, which a Windows
install does), restoring `agent.yaml` without the store also gives the
agent a new identity.

Whether the store can move to another machine depends on its backend:

| Backend | Restores on the same machine | Restores on another machine |
|---|---|---|
| `age-keyfile` (Linux default) | Yes | Yes, with `agent-secret.key` and `secrets.age` copied together |
| `systemd-creds` | Yes | No: the credentials are sealed with the host key (and the TPM when present) |
| `dpapi` (Windows) | Yes | No: DPAPI machine scope decrypts only on the machine that sealed |

To move an agent whose store cannot follow, list the secrets on the old
machine (`senhub-agent secret list`), and set each of them again on the
new one with `senhub-agent secret set <name>`. The references in the
fragments stay as they are.

## The console rewrites the fragments

The web console creates, rewrites and deletes files under `probes.d/`
and `strategies.d/`, and deleting a probe there also removes that
probe's sealed secrets. A backup taken before a console session does not
describe the agent after it: restoring it brings back probes an operator
removed, with references to secrets that no longer exist.

Take the backup after the last change, or take one per change.

## Restoring

1. Install the same version of the agent, or a later one.
2. Stop the service.
3. Put the configuration directory back, the store files included, and
   the state directory.
4. Run `senhub-agent config check`. It reads the fragments, resolves the
   `${secret:}` references and says which cannot be resolved.
5. Start the service and read `senhub-agent status`.

On Windows the uninstaller removes `C:\ProgramData\SenHub\`: copy it
aside before uninstalling, not after.
