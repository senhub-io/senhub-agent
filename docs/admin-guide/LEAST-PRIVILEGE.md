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
NoExecPaths=/var/lib/senhub-agent /var/log/senhub-agent
CapabilityBoundingSet=
AmbientCapabilities=
SupplementaryGroups=systemd-journal

ProtectClock=true
ProtectHostname=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true

RestrictNamespaces=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true
MemoryDenyWriteExecute=true
RemoveIPC=true
UMask=0077

SystemCallArchitectures=native
SystemCallFilter=@system-service
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
```

All Linux capabilities are dropped by default; the agent joins the
`systemd-journal` group so the `linux_logs` probe can read the journal
without root.

Two of those deserve a word. `RestrictAddressFamilies` allows only the
four families the agent actually opens — notably **not** `AF_PACKET`,
which is the one a compromised monitoring agent would want most, since
it is raw frame capture on every interface. And `NoExecPaths` marks the
agent's own directories non-executable: the agent must be able to write
its state and its logs, and this stops that necessary write access from
doubling as a place to drop a payload and run it.

See [Verifying the security posture](#verifying-the-security-posture)
for how to check all of this on your own host rather than taking this
page's word for it.

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
sudo senhub-agent update <version>   # installs the binary (the daemon cannot)
```

Inspection commands (`version`, `status`, `config check`,
`config show`) never require elevation.

## One binary, which the daemon cannot write

The agent is on disk **once**, at a root-owned path:

| Layout | Path | Owner |
|---|---|---|
| `senhub-agent install` (ZIP / tarball) | `/usr/local/bin/senhub-agent` | `root` |
| `.deb` / `.rpm` / `.zypper` package | `/usr/bin/senhub-agent` | `root` |

Both sit inside `ProtectSystem=full`'s read-only tree, so the `senhub`
service account cannot modify the binary systemd executes. That is
deliberate, and it is the reason the daemon no longer updates itself on
Linux.

**Why.** The daemon is the part of the agent exposed to input you do not
control: OTLP arriving over the network, SNMP traps, syslog, tailed log
files, and the responses of every target it probes. It is therefore the
component most likely to be compromised. A daemon able to rewrite its
own executable hands whoever compromises it *persistence across
restarts* — on an agent that runs everywhere and restarts itself, that
is the outcome worth attacking for.

The signature check does not close that gap. It runs inside the same
process, so an attacker who controls the daemon controls the code doing
the checking. Verification performed by the party that may be
compromised is not verification.

So installation is left to something the daemon cannot influence.

### Updating

```bash
sudo senhub-agent update <version>     # today
sudo apt upgrade senhub-agent          # once packages are published
```

The daemon still **checks** for new versions when `auto_update.enabled`
is set. It reports what it finds and names the command that applies it;
it does not install. In the journal:

```
A newer version is available. The agent does not install it itself on Linux:
the binary is root-owned so the service account cannot rewrite it.
Apply it with 'sudo senhub-agent update', ...
```

This is what package-managed agents do, and it is the shape the `.deb` /
`.rpm` / SUSE packages slot into unchanged: the package manager verifies
against the system keyring, installs as root, and records what it
installed so `debsums` or `rpm -V` can verify it afterwards.

> **Windows is different and keeps in-process updates.** An MSI install
> stages a signed MSI and hands the upgrade to `msiexec`, a privileged
> installer outside the agent — the same separation described above,
> reached by a different road. A ZIP install replaces its own binary,
> which escalates nothing there because the service runs as LocalSystem
> and already holds the highest privilege on the machine.

### Upgrading from a pre-0.5.4 install

Earlier versions carried the agent **twice**: the copy you ran, and a
`senhub`-owned copy under `/var/lib/senhub-agent/bin` that the unit
executed so the daemon could replace it during auto-update. The two
drifted apart as soon as auto-update ran.

`sudo senhub-agent install` (or `sudo senhub-agent refresh-unit`) moves
such a host to the single-binary layout: it installs the root-owned
binary, repoints `ExecStart`, and removes the old directory.

Two things to know before you upgrade a host:

- **The unit and the binary move together.** `NoExecPaths` makes
  anything under `/var/lib/senhub-agent` non-executable, so a host that
  receives the new unit while its `ExecStart` still points at the old
  copy fails to start with `203/EXEC` and restarts until the start
  limiter stops it. `install` and `refresh-unit` rewrite both at once;
  do not hand-copy one without the other.
- **`refresh-unit` normally preserves a custom `ExecStart`**, so that a
  path you chose survives a refresh. The old
  `/var/lib/senhub-agent/bin/senhub-agent` is the single exception: it
  is recognised by name and repointed even though the file is still
  there, because it is not a path anyone chose — it is a layout we
  shipped and are migrating off. A genuinely custom path, such as
  `/opt/senhub/bin/senhub-agent`, is still left alone.

## Verifying the security posture

Do not take this page's word for it. `systemd-analyze` scores the
running unit against the full set of systemd restrictions:

```bash
systemd-analyze security senhub-agent
```

Measured on Ubuntu 26.04 (systemd 259), with the shipped unit and no
site-local drop-ins:

| Unit | Exposure |
|---|---|
| Before 0.5.4 | **5.9 MEDIUM** |
| 0.5.4 | **2.0 OK** |

The remaining exposure is mostly intrinsic to what a monitoring agent
is, and the score will not reach zero without removing the product:

| Still open | Why it stays |
|---|---|
| `AF_INET` / `AF_INET6` / `AF_UNIX` / `AF_NETLINK` sockets | probes talk to targets; host network metrics come from netlink |
| `ProtectProc=` / `ProcSubset=` | the `process` probe reports on the process tree it would hide |
| `PrivateDevices=` / `DeviceAllow=` | `smart`, `nvidia` and `ipmi` read `/dev/sd*`, `/dev/nvme*`, `/dev/ipmi0` |
| `SupplementaryGroups=` | `systemd-journal` for `linux_logs`, `adm` for `filetail` on syslog |

If you run **none** of the hardware probes, adding
`PrivateDevices=true` in a drop-in is safe and buys 0.2.

### What raises your exposure, and by how much

Anything you add in a drop-in is yours to justify. The two that cost the
most:

| Addition | Cost | What it actually grants |
|---|---|---|
| `CAP_SYS_PTRACE` | 0.3 | attach a debugger to other processes |
| `CAP_DAC_READ_SEARCH` | 0.2 | **bypass every file read permission check on the host** — `/etc/shadow`, private keys, customer data included |

`CAP_DAC_READ_SEARCH` deserves the emphasis. It is not "read a few more
log files"; it is unrestricted read of the filesystem, granted to a
process that parses untrusted network input.

Two features ask for it, and both are opt-in and off by default:

- **Socket-to-process attribution** (the host dependency source) needs
  `CAP_SYS_PTRACE` **and** `CAP_DAC_READ_SEARCH` together, to read
  `/proc/<pid>/fd` of processes owned by other users. Enabling it means
  accepting that trade; on a host where it is not worth it, leave the
  dependency source off.
- **Reading `/var/log/syslog` with `filetail`** does *not* need it — use
  the `adm` group instead, which grants exactly those files and nothing
  else. See [above](#reading-the-system-log-files-filetail).

> **Windows:** the daemon still runs with administrator privileges;
> the non-root work described here is Linux-specific.
