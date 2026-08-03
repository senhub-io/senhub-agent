# Running the agent least-privilege (non-root)

The SenHub Agent daemon does **not** require root on Linux. The `.deb`
and `.rpm` packages — and, since 0.2.3, the `senhub-agent install` CLI
command — install it to run as a dedicated, unprivileged system user
(`senhub`) under a hardened systemd unit. Only the service-lifecycle
commands (`install`, `uninstall`, `start`, `stop`, `restart`) need
root, because they register and control the systemd unit and own the
on-disk install.

Running a long-lived, network-facing daemon as root widens the blast
radius of any vulnerability from "service account" to "host root".
Running it as `senhub` with targeted capabilities is the
defense-in-depth posture security and compliance reviews (CIS, NIST
least-privilege) expect — and what peer agents (node_exporter,
OpenTelemetry Collector, Datadog agent, Telegraf) do.

## What the installers set up for you

Installing the `.deb` / `.rpm`, or running `senhub-agent install`
from a ZIP install, performs all of the following so the agent starts
unprivileged out of the box:

| Item | Value |
|---|---|
| System user / group | `senhub` / `senhub` (system account, `nologin` shell, no home) |
| Config directory | `/etc/senhub-agent` (config `0640`, owned by `senhub`) |
| State directory | `/var/lib/senhub-agent` (`0750`, owned by `senhub`) |
| Log directory | `/var/log/senhub-agent` (`0750`, owned by `senhub`) |
| systemd unit | `senhub-agent.service` with `User=senhub` |

The unit applies these hardening directives:

```ini
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/senhub-agent /var/log/senhub-agent
CapabilityBoundingSet=
AmbientCapabilities=
SupplementaryGroups=systemd-journal
```

All Linux capabilities are dropped by default; the agent joins the
`systemd-journal` group so the `linux_logs` probe can read the journal
without root.

## Per-probe privilege map

Most probes need nothing beyond the unprivileged service account. The
exceptions, and how to grant exactly what they need:

| Probe / feature | Needs | How to grant (non-root) |
|---|---|---|
| `cpu`, `memory`, `logicaldisk`, `network` | nothing | reads `/proc`, `/sys` — works as-is |
| `linux_logs` | journal read | `systemd-journal` group (set in the shipped unit) |
| `snmp_trap` on the default UDP **162** | bind a privileged port | `CAP_NET_BIND_SERVICE`, or use a high port |
| `otlp_receiver` on 4317 / 4318 | nothing | ports are above 1024 |
| ICMP / ping active checks | raw sockets | `CAP_NET_RAW` |
| Remote probes (databases, NetScaler, Veeam, SNMP poll, …) | network + credentials | no host privilege; credentials in config |

### Prefer a high port over a capability

The simplest way to avoid any capability is to not bind a privileged
(<1024) port. For `snmp_trap`, bind a high port and have your senders or
a forwarder target it:

```yaml
probes:
  - type: snmp_trap
    name: trap_receiver
    params:
      bind_address: "0.0.0.0:16200"   # high port — no capability needed
```

### Granting a capability when you must

If a probe genuinely needs a privileged port or raw sockets, grant the
single capability in a unit drop-in instead of reverting to root:

```bash
sudo systemctl edit senhub-agent.service
```

```ini
[Service]
# snmp_trap on UDP/162
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
```

```bash
sudo systemctl daemon-reload
sudo systemctl restart senhub-agent.service
```

The shipped unit lists these lines commented for reference.

## CLI installs (`senhub-agent install`) and migration

Since 0.2.3, `senhub-agent install` writes the same hardened unit the
packages ship (it embeds the packaged unit, re-templating only
`ExecStart` / `WorkingDirectory` to the actual binary location), and
creates the `senhub` user/group if missing. The generated config, log
and certificate files are handed to the service user during install.

To keep the daemon running as root — for example while a probe that
relied on blanket root is migrated to a targeted capability — install
the legacy unit explicitly:

```bash
sudo senhub-agent install --user root
```

### Existing installs are not changed on upgrade

`install` never overwrites an existing `senhub-agent.service` unit:
re-running it over a registered service fails with "Init already
exists". A binary upgrade (auto-update or manual replace) keeps the
unit — and therefore the user — you installed with. The hardened unit
applies only after an explicit `uninstall` + `install`; before doing
that on a host that ran as root, check the per-probe privilege map
above for anything that needs a capability drop-in (privileged ports,
raw ICMP sockets).

## Running manually as a non-root user

`senhub-agent run` works as any user that can read its config and write
its state/log paths:

```bash
sudo -u senhub /usr/bin/senhub-agent run \
  --config-path /etc/senhub-agent/agent.yaml
```

If a path is not accessible the agent fails with an error naming the
path — fix ownership (`chown senhub:senhub …`) rather than running as
root.

## What still needs root

Service management touches systemd and the install tree, so these
commands keep the root requirement:

```bash
sudo senhub-agent install      # register + enable the service
sudo senhub-agent start|stop|restart
sudo senhub-agent uninstall
sudo senhub-agent update <version>   # replaces binaries on disk
```

Inspection commands (`version`, `status`, `config check`,
`config show`) never require elevation.

## The two binaries, and keeping them in sync

A hardened install carries the agent binary twice:

| Path | Owner | Role |
|---|---|---|
| `/usr/local/bin/senhub-agent` (or wherever you installed it) | `root` | the CLI operators invoke |
| `/var/lib/senhub-agent/bin/senhub-agent` | `senhub` | the binary the unit execs (`ExecStart`) |

This is a consequence of running the daemon unprivileged, not an
accident. Self-update replaces a binary by writing a sibling file and
renaming it over the target, so the daemon needs write access to its own
binary **and** its directory. A root-owned binary under
`/usr/local/bin` can never be replaced by a `senhub` process — and must
not be, or the service account could plant a binary that root later
executes. So the installer stages a copy the service owns, inside its
`StateDirectory`, and the unit execs that one.

The consequence is that the daemon updates only its own copy. Nothing
lets it refresh the CLI copy, so the two drift as soon as auto-update
runs — which is why the fleet routinely showed `senhub-agent --version`
reporting an old release while the service ran a current one.

Two behaviours close that gap:

- **`sudo senhub-agent update <version>` reconciles both.** It runs as
  root, so it is the one path that can write either file. After
  replacing the CLI copy it copies the new release over the unit's
  `ExecStart` target and hands ownership back to the unit's `User=`, so
  the daemon can still self-update afterwards. The command names both
  files it wrote. Re-running it is also the repair for a host whose
  service copy fell behind.

  It refuses one case: a service copy running a **newer** version than
  the release being installed is left untouched and reported, rather
  than silently downgraded (the daemon legitimately runs ahead of the
  CLI).

- **`senhub-agent --version` reports the skew.** When the service execs
  a different build, the version output names it:

  ```
  Version: 0.5.3 (commit: a8e67f7)
  Service binary: 0.5.4 (/var/lib/senhub-agent/bin/senhub-agent)
  Note: the systemd service runs a different build than this CLI binary.
        'sudo senhub-agent update <version>' updates both copies.
  ```

  The version is read from the other binary's build metadata; it is
  never executed. Running a service-user-owned binary as root would
  reintroduce exactly the escalation this layout prevents.

A legacy root install whose unit execs the binary in `PATH` has a single
copy, so neither behaviour has anything to reconcile and both stay
silent.

> **Windows:** the daemon still runs with administrator privileges;
> the non-root work described here is Linux-specific.
