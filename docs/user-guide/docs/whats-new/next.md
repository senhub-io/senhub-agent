# Next (unreleased)

Changes land here as they are merged.

<div class="rn-filter"></div>

## Before you upgrade

- **PRTG sensors no longer carry the Check Result channel by default.** 0.6.4 added it to every database, application and active-check probe, referencing the `senhub.probe.check.result` lookup; on a PRTG core without the SenHub lookup files the sensor turned Warning (PE272). The channel is now off unless `prtg.check_result_channel: true` is set in the `http` output, and then it keeps the same lookup name and values. PRTG keeps a channel that stops receiving data: if you installed the lookups and want to keep the channel, set the option before upgrading (0.6.4 runs with it and ignores it; only its `config check` reports it as unread); otherwise delete the channel or re-add the sensor. See [Installing PRTG value lookups](../web-interface.md#installing-prtg-value-lookups).

## Features

- **Console: a page per probe, and series on the Overview.** `probes?view=<name>` shows one probe's state and full failure reason, current series, read-only configuration and recent events, and every probe or output row on the Overview and the Probes list now opens its page. The Overview adds a "Series exported" card (per output, top probes, created since start) when the build meters series, backed by the new `GET /api/{key}/info/self-records`. See [Web console](../web-interface.md).
- **Proxmox VE: disk health, ZFS, backup and task series.** The `proxmox` probe now reports physical disk SMART state and SSD life remaining (`hw.physical_disk.status`, `hw.physical_disk.endurance_utilization`), ZFS pool state, size, usage and fragmentation (`hw.logical_disk.*`, `proxmox.zfs.pool.fragmentation`), the age and outcome of each guest's last backup, the number of guests covered by no backup job and of enabled jobs, storage availability, the count of failed tasks, each failure also published as an ERROR log record, the state of HA-managed guests and cluster quorum (`proxmox.ha.resource.status`, `proxmox.ha.quorum.status`), and the status and last-sync age of storage replication jobs (`proxmox.replication.job.status`, `proxmox.replication.job.age`). Status series carry one point per state (1 for the current state, 0 for the others).

- **Azure Monitor probe (Pro).** The new `azure_monitor` probe discovers the resources of one or more subscriptions through Azure Resource Graph, filtered by type, resource group and tag, and publishes their platform metrics, their provisioning and product state, and optional Service Bus, App Service Plan and VM disk figures. Entity emission is off by default.
- **Azure Log Query probe (Pro).** The new `azure_log_query` probe runs one KQL query on a Log Analytics workspace each cycle and publishes every row (`channel`, `value`, `unit`) as a gauge named `azure.log_query.<channel>`.
- **Microsoft Entra ID probe (Pro).** The new `entra_id` probe reads one tenant through Microsoft Graph in 13 modules: secret and certificate expiry, directory synchronisation, users, licences, groups, applications, devices, service health, authentication methods, privileged access and security posture. Each module needs its own Application permission with admin consent.
