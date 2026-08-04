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
| `filetail` on `/var/log/syslog`, `auth.log` | read `adm`-owned files | `adm` group (joined by the installer, see below) |
| `snmp_trap` on the default UDP **162** | bind a privileged port | `CAP_NET_BIND_SERVICE`, or use a high port |
| `otlp_receiver` on 4317 / 4318 | nothing | ports are above 1024 |
| ICMP / ping active checks | raw sockets | `CAP_NET_RAW` |
| Remote probes (databases, NetScaler, Veeam, SNMP poll, …) | network + credentials | no host privilege; credentials in config |

### Reading the system log files (`filetail`)

The journal is covered by the shipped unit, but the classic log files are
not readable by an unprivileged account: on Debian/Ubuntu
`/var/log/syslog` and `/var/log/auth.log` are `syslog:adm 0640`. A
`filetail` probe pointed at them collects nothing as `senhub`.

The installer (`senhub-agent install`, and the `.deb` / `.rpm`
postinstall) joins the service user to `adm` for this, so it works out of
the box. `senhub-agent refresh-unit` performs the same join, which is how
an install predating this behaviour is repaired. To do it by hand:

```bash
sudo usermod -aG adm senhub
sudo systemctl restart senhub-agent.service
```

The membership is granted through the **user database**, not the unit's
`SupplementaryGroups=`, on purpose: systemd honours the user's static
groups, whereas a `SupplementaryGroups=` naming a group that does not
exist on the distribution fails the unit at startup with `216/GROUP`.
Where `adm` is absent, the join is skipped and the install still
succeeds.

> **Do not use `CAP_DAC_READ_SEARCH` for this.** It does make the logs
> readable, which is why it gets reached for — but it bypasses *every*
> file read permission check on the host, so the agent can then read
> `/etc/shadow`, private keys and any customer data on the machine. The
> `adm` group grants those log files and nothing else.

> **Red Hat family:** `rsyslog` writes `/var/log/messages` as
> `root:root 0600` there, so `adm` does not help. Read the journal with
> `linux_logs` instead, which needs no extra grant.

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
```

Inspection commands (`version`, `status`, `config check`,
`config show`) never require elevation.

> **Windows:** the daemon still runs with administrator privileges;
> the non-root work described here is Linux-specific.
