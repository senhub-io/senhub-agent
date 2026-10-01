# Next (unreleased)

Nothing released yet since 0.6.1. Changes land here as they are merged to `dev`.

<div class="rn-filter"></div>

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
