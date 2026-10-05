# Next (unreleased)

Changes land here as they are merged to `dev`.

- **Redis**: the probe now reports the last RDB save, AOF rewrite and AOF write outcomes, the replication backlog (active, size, history), pub/sub channels and patterns, and, on a Sentinel, the status, replica count and sentinel count of each monitored master.
- **Linux packages.** `.deb` and `.rpm` packages for amd64 and arm64 install the agent as a systemd service from the distribution's package manager, tested on Debian 12, Ubuntu 22.04 and 24.04, Rocky Linux 9 and openSUSE Leap 15.6; see [Install from packages](../installation.md#install-from-packages). Repositories are announced.

<div class="rn-filter"></div>

## Features

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
- **Zabbix links the host probes of each platform by default.** `zabbix setup` linked five templates whatever the platform, so a Windows host collecting its services or its event log counter got no item for them. The probes marked `universal` in their definition (on Windows, `winservices` and `windows_eventlog` join the five) are linked per platform, `--all-probes` links every template, and a guard fails the build if a registered probe emits metrics and no template declares them (ent#109).
- **Zabbix templates ship graphs and dashboards.** The CPU, memory, logical disk and network templates carry graph prototypes (utilization, load, throughput, operations, queue, errors, per instance) declared in the probe definitions under `graphs:`, and a dashboard of their own graphs per template, listed under the host's Dashboards menu (7.0 export; the 6.0 export keeps the graphs only) (ent#110).
