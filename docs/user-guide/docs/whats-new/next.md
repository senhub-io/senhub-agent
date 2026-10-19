# Next (unreleased)

Changes land here as they are merged.

<div class="rn-filter"></div>

## Before you upgrade

- **PRTG sensors no longer carry the Check Result channel by default.** 0.6.4 added it to every database, application and active-check probe, referencing the `senhub.probe.check.result` lookup; on a PRTG core without the SenHub lookup files the sensor turned Warning (PE272). The channel is now off unless `prtg.check_result_channel: true` is set in the `http` output, and then it keeps the same lookup name and values. PRTG keeps a channel that stops receiving data: if you installed the lookups and want to keep the channel, set the option before upgrading (0.6.4 runs with it and ignores it; only its `config check` reports it as unread); otherwise delete the channel or re-add the sensor. See [Installing PRTG value lookups](../web-interface.md#installing-prtg-value-lookups).
- **Secrets are masked in log records by default.** Every log record now goes through a masking step before any output receives it: the value of `password`, `passwd`, `pwd`, `passhash`, `apikey`, `api_key`, `token`, `access_token` and `secret` parameters (query strings, form bodies, `key=value` text, JSON) becomes `***`, and the credential of an `Authorization:` header is masked while its scheme is kept. This covers, for instance, a PRTG `passhash=` that a reverse-proxy access log shipped by `filetail` used to carry in clear. Records that contain none of these keys are unchanged. Masked occurrences are counted in `senhub.agent.logs.redacted` by probe and rule. To turn the built-in rules off, set `logs.redact.preset: false`; to mask more, add `logs.redact.rules` (name, RE2 pattern, replacement). See [Logs Section](../configuration.md#logs-section).

## Features

- **Proxmox VE: disk health, ZFS, backup and task series.** The `proxmox` probe now reports physical disk SMART state and SSD life remaining (`hw.physical_disk.status`, `hw.physical_disk.endurance_utilization`), ZFS pool state, size, usage and fragmentation (`hw.logical_disk.*`, `proxmox.zfs.pool.fragmentation`), the age and outcome of each guest's last backup, the number of guests covered by no backup job and of enabled jobs, storage availability, the count of failed tasks, each failure also published as an ERROR log record, the state of HA-managed guests and cluster quorum (`proxmox.ha.resource.status`, `proxmox.ha.quorum.status`), and the status and last-sync age of storage replication jobs (`proxmox.replication.job.status`, `proxmox.replication.job.age`). Status series carry one point per state (1 for the current state, 0 for the others).
