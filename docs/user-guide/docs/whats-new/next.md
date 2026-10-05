# Next (unreleased)

Nothing released yet since 0.6.1. Changes land here as they are merged to `dev`.

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
