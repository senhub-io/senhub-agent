# Running the agent least-privilege (non-root)

The SenHub Agent daemon does **not** require root on Linux. The `.deb`
and `.rpm` packages install it to run as a dedicated, unprivileged
system user (`senhub`) under a hardened systemd unit. Only the
service-lifecycle commands (`install`, `uninstall`, `start`, `stop`,
`restart`) need root, because they register and control the systemd
unit and own the on-disk install.

Running a long-lived, network-facing daemon as root widens the blast
radius of any vulnerability from "service account" to "host root".
Running it as `senhub` with targeted capabilities is the
defense-in-depth posture security and compliance reviews (CIS, NIST
least-privilege) expect — and what peer agents (node_exporter,
OpenTelemetry Collector, Datadog agent, Telegraf) do.

## What the packages set up for you

Installing the `.deb` / `.rpm` performs all of the following so the
agent starts unprivileged out of the box:

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
