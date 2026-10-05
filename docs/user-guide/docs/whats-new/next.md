# Next (unreleased)

Changes land here as they are merged to `dev`.

- **Redis**: the probe now reports the last RDB save, AOF rewrite and AOF write outcomes, the replication backlog (active, size, history), pub/sub channels and patterns, and, on a Sentinel, the status, replica count and sentinel count of each monitored master.
- **Linux packages.** `.deb` and `.rpm` packages for amd64 and arm64, in two editions (`senhub-agent-oss` and `senhub-agent`, which replace each other in one install command), install the agent as a systemd service from the distribution's package manager, tested on Debian 12, Ubuntu 22.04 and 24.04, Rocky Linux 9 and openSUSE Leap 15.6; see [Install from packages](../installation.md#install-from-packages). Signed APT and YUM/DNF/Zypper repositories at `packages.senhub.io` go live with the first published beta; see [Install from the package repositories](../installation.md#install-from-the-package-repositories).

- **IPMI**: sensors that share a name (a Dell lists every CPU temperature as `Temp`) are no longer merged into one series; they now read `Temp (CPU 1)`, `Temp (CPU 2)`. Power (`hw.power`, watts) and current (`senhub.hardware.current`, amperes) readings are reported, and a power supply's presence and redundancy ("Presence detected", "Fully Redundant", "Redundancy Lost") now give its status.

<div class="rn-filter"></div>

## Features

- **`status` works on every install.** The agent now answers `senhub-agent status` on a local channel (a Unix socket in its state directory, readable by the service account and root only; a named pipe restricted to administrators on Windows), whether or not the HTTP output is enabled. Hosts installed before the HTTP output was on by default used to get a degraded view computed by the command itself. `status` asks the local channel first and falls back to the HTTP output. The channel is read-only: it sends the status and reads nothing. It reports probe health and failed outputs but not the per-probe metric counts, which only the HTTP cache holds.
- **Two agents on one host no longer describe each other twice.** Each agent
  writes its own `service.instance.id` to `instance.id` in its state
  directory (`/var/lib/senhub-agent`, `C:\ProgramData\SenHub`), readable by
  other accounts only when the agent key is a random UUID, and by its owner
  alone otherwise. An agent that finds another agent's process listening on
  the host reuses that id instead of creating a second `service.instance`.
  When the file cannot be read, the previous behaviour applies.
- **Redfish: the whole probe now reaches Prometheus and OTLP.** 100 of the
  139 metrics the probe can emit had no definition and were dropped by
  those outputs (processors, memory modules, network, power, firmware,
  event-log counters, vendor storage readings). They are now declared with
  OTel names, units and types, and a test fails when the probe starts to
  emit a name that has none. PRTG channel names are unchanged. Memory
  modules, adapters, ports and cache levels also become separate series on
  the pull sinks instead of overwriting each other.
- **Redfish: `collections` is validated and says what it turns off.** The
  list still replaces the default set, but an unknown name or an empty list
  now stops the probe at load with the accepted values, and the start-up
  log names the default subsystems the list disabled. A configuration that
  lists the six defaults is unchanged.
- **Redfish: names keep the BMC's own text.** `hw.name` and the other
  name attributes in OTLP, Prometheus and Zabbix are no longer stripped of
  `, ; ( ) [ ] { } < > | \ " ' ` # & ? =`. PRTG channel names and URL
  filters keep their cleaned form.

- **Linux host security signals in the `process` probe.** Three new
  machine-wide metrics on Linux: `senhub.system.kernel.open_files` (file
  handles allocated, from `/proc/sys/fs/file-nr`, to read against the
  existing `senhub.system.kernel.max_files`), `senhub.system.passwd.checksum`
  (CRC32 of `/etc/passwd` as a number that changes when the file changes)
  and `senhub.system.passwd.modified_timestamp` (its modification time).
  The generated Zabbix template for the probe carries a warning trigger
  that fires when the checksum changes, `change(...)<>0`. Re-run
  `zabbix setup` to refresh the templates. Other platforms emit nothing
  new.
## Before you upgrade

- **Three entity identities change once.** The `unifi`, `kubernetes` and
  `systemd` probes no longer key their entities on an address or a hostname.
  A UniFi controller is identified by the UUID it reports about itself (or
  `unifi@<host.id>` when it runs on the agent's host); a controller that is
  remote and whose UUID the account cannot read has no entity. A Kubernetes
  cluster is identified by its `kube-system` namespace UID alone, so a
  cluster whose UID is unreadable has no cluster entity. A systemd unit is
  `systemd://<host.id>/<unit>`. The old entities are retired once: a
  consumer sees one disappearance, then the new identity.
- **Zabbix counter items become rates.** In the generated templates, every
  item built from a cumulative counter (network bytes and packets, disk
  I/O, CPU time, request totals) now carries the Change per second
  preprocessing step and a per-second unit (`Bps`, `/s`, `s/s`). The
  items keep their keys, so after you re-run `zabbix setup` and the
  templates are updated, the same item switches from a total to a rate:
  its history from before the upgrade holds totals and the new values are
  rates, so a graph spanning the upgrade shows a step. Rewrite the
  triggers and calculated items you wrote against these items to compare
  a rate.
## Features

- **Linux disk I/O.** The `logicaldisk` probe now reports, per whole block
  device, the bytes and operations read and written and the time spent on
  I/O (`system.disk.io`, `system.disk.operations`, `system.disk.io_time`),
  as cumulative counters, until now a Windows-only measurement. Partitions
  and `loop`, `ram`, `zram`, `fd` and `sr` devices are left out. They reach
  Prometheus, OTLP and Zabbix, which discovers one set of items per device.
- **Host clock.** The `cpu` probe reports the host's time
  (`senhub.system.time`, seconds since the Unix epoch) on Linux and Windows,
  so a monitoring server can check clock drift: `time() - senhub_system_time_seconds`
  in Prometheus, and in Zabbix a `fuzzytime()` trigger whose tolerance is
  `{$SENHUB.CLOCK.DRIFT.MAX}` (60 seconds by default).
## Breaking Changes

- **Exit codes of the command line are a contract.** `0` done, `1`
  warning, `2` failure, `3` unchanged. Failures that exited `1` now exit
  `2`, and `config check` exits `1` when it found warnings only (running
  without a licence is the free tier, reported as information) and `status` exits `1` when the service is stopped, the
  agent does not answer, it reports itself unhealthy or a probe is in
  error. A script that
  tests for any non-zero code is unaffected; one that compares with `1`
  must follow. The container image and the Windows installer are updated
  to match. See [Exit codes](../cli.md#exit-codes).

## Features

- **`doctor` diagnoses an install in one pass.** It aggregates the
  service state, the systemd unit and binary comparison, `config check`,
  the secret store, the licence expiry, the HTTP port, the connection
  test of every output, the probes' last cycle and the disk, prints a
  `fix` for each problem, and exits `1` on a warning and `2` on a
  failure. It needs no root and works with the service stopped.
  `--json` prints `senhub.cli.doctor/v1`. See [Doctor](../cli.md#doctor).
- **`--json` output.** `version`, `status`, `config check`, `config
  show`, `config set`, `config init` and `secret status` print one JSON
  object with a `senhub.cli.<command>/v1` schema identifier. Failures
  are JSON objects too. See [JSON output](../cli.md#json-output).
- **Idempotent provisioning commands.** `config init`, `config set` and
  `install` run again on a machine already in the requested state write
  nothing and exit `3`. `config init --ok-if-unchanged` exits `0` in that
  case, for installers that treat any other code as a failure.
- **filetail reports where each tail stands.** For every file followed, `senhub.filetail.read_offset` and `senhub.filetail.file_size` (Prometheus `senhub_filetail_read_offset_bytes` and `senhub_filetail_file_size_bytes`, attribute `log.file.path`) let a rule detect a frozen tail: the file grew and the offset did not move.

## Fixes

- **A per-module debug level now writes debug lines.** Raising one module
  (for example `probe.ibmi`) to `debug` through the log-level API or the
  console answered `200` but wrote nothing, because the production logger
  kept a global `info` floor. The module now logs at debug while every other
  module stays at `info`. The setting is kept in memory: it survives a
  configuration reload and is lost on an agent restart (see
  [Troubleshooting](../troubleshooting.md)).
- **Oracle 23ai: login with a password longer than 30 characters.** The
  `oracle` probe could not log in to Oracle Database 23ai with a password of
  more than 30 characters: every cycle reported `senhub.db.up = 0` with
  `ORA-01017`, while SQL\*Plus accepted the same credentials. The driver does
  not announce long password support, which 23ai requires; the probe now does.
  Passwords of 30 characters or fewer were never affected.
- **Zabbix links the host probes of each platform by default.** `zabbix setup` linked five templates whatever the platform, so a Windows host collecting its services or its event log counter got no item for them. The probes marked `universal` in their definition (on Windows, `winservices` and `windows_eventlog` join the five) are linked per platform, `--all-probes` links every template, and a guard fails the build if a registered probe emits metrics and no template declares them (ent#109).
- **Zabbix templates ship graphs and dashboards.** The CPU, memory, logical disk and network templates carry graph prototypes (utilization, load, throughput, operations, queue, errors, per instance) declared in the probe definitions under `graphs:`, and a dashboard of their own graphs per template, listed under the host's Dashboards menu (7.0 export; the 6.0 export keeps the graphs only) (ent#110).
- **Windows services are discovered in Zabbix, with an "automatic service not running" trigger.** The `winservices` probe now reports each service's start type (`windows.service.start_type`); the template creates the state, status and start type of every service by discovery and raises a problem when a service set to start automatically has not been running for `{$SENHUB.WINSERVICES.GRACE}` (5 minutes). Discovery is filtered by `{$SENHUB.WINSERVICES.MATCHES}` and `{$SENHUB.WINSERVICES.NOT_MATCHES}`. The probe's own heartbeat is no longer declared once per service (ent#114).
- **Zabbix triggers cover more of a host.** Processor queue (Windows) and load (Linux), paging, disk queue and busy time, inodes, interface errors and discards, and a restart (new `system.uptime` from the `cpu` probe) join the usage and state triggers, each limit a `{$SENHUB.*}` macro with a documented default. Triggers are declared in the probe definitions under `triggers:` (ent#115).
