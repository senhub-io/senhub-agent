<img src="../../assets/probe-logos/proxmox.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Proxmox VE

The `proxmox` probe monitors a Proxmox VE cluster via the REST API, reporting
per-node CPU and memory utilization and status, per-VM (QEMU) and LXC container
CPU, memory, disk I/O, network throughput and running state, and storage pool
usage.

## Quick start

```yaml
# probes.d/10-proxmox.yaml — each file under probes.d/ is a YAML array of probes
- name: proxmox
  type: proxmox
  params:
    endpoint: https://pve.example.com:8006
    token_id: monitor@pve!agent
    token_secret: ${secret:proxmox.token_secret}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:ab29f8261ab0433230e24ee32a418dca0590d9edcc788f1231254cb1c561d9ca -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | HTTPS base URL of the cluster API. Example: `https://pve.example.com:8006` |
| `token_id` | Yes | - | API token identifier as user@realm!tokenname. Example: `monitor@pve!agent` |
| `token_secret` | Yes | - | API token secret. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `verify_tls` | No | `true` | Verify the API certificate; false accepts a self-signed one |
| `node` | No | - | Only this node is collected; empty means every node of the cluster |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `15` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity override for this cluster |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.proxmox.up` | 1 | 1 when the Proxmox API answered successfully |
| `proxmox.node.cpu.utilization` | 1 | Node CPU utilization ratio (0–1), tagged with `proxmox.node` |
| `proxmox.node.memory.used` | By | Node memory in use |
| `proxmox.node.memory.limit` | By | Node total memory |
| `proxmox.node.status` | 1 | Node online state: 1 = online, 0 = offline |
| `proxmox.vm.cpu.utilization` | 1 | VM CPU utilization, tagged with `proxmox.vmid` / `proxmox.vm.name` |
| `proxmox.vm.memory.used` | By | VM memory used |
| `proxmox.vm.disk.read` | By | VM block I/O bytes read (monotonic) |
| `proxmox.vm.disk.write` | By | VM block I/O bytes written |
| `proxmox.vm.network.in` | By | VM network bytes received |
| `proxmox.vm.network.out` | By | VM network bytes transmitted |
| `proxmox.vm.status` | 1 | VM state: 1 = running, 0 = stopped/other |
| `proxmox.storage.used` | By | Storage pool space used, tagged with `proxmox.storage` |
| `proxmox.storage.limit` | By | Storage pool total capacity |
| `proxmox.storage.status` | 1 | Storage availability, one series per `proxmox.storage.state` (`active`, `inactive`): 1 for the current state, 0 for the other |
| `hw.physical_disk.status` | 1 | SMART state of a physical disk, one series per `hw.state` (`ok`, `degraded`, `failed`, `predicted_failure`): 1 for the current state, 0 for the others. `hw.id` is the serial number (device path when there is none) |
| `hw.physical_disk.endurance_utilization` | 1 | SSD life remaining as a ratio (the PVE wearout percentage divided by 100, `hw.physical_disk.state` = `remaining`) |
| `hw.logical_disk.status` | 1 | ZFS pool state, one series per `hw.state` (`ok`, `degraded`, `failed`). ONLINE is `ok`, DEGRADED `degraded`; FAULTED, UNAVAIL, REMOVED, SUSPENDED and OFFLINE are `failed` |
| `hw.logical_disk.limit` | By | ZFS pool size, `hw.id` = pool name |
| `hw.logical_disk.usage` | By | ZFS pool bytes, `hw.logical_disk.state` = `used` or `free` |
| `hw.logical_disk.utilization` | 1 | ZFS pool used over size (0–1) |
| `proxmox.zfs.pool.fragmentation` | 1 | ZFS pool fragmentation as a ratio (0–1) |
| `proxmox.vm.backup.age` | s | Seconds since the last successful backup of a guest |
| `proxmox.vm.backup.status` | 1 | Outcome of the last backup task naming the guest, one series per `proxmox.vm.backup.state` (`ok`, `failed`) |
| `proxmox.vm.backup.unprotected.count` | {vm} | Guests covered by no enabled backup job |
| `proxmox.backup.job.count` | {job} | Enabled backup jobs |
| `proxmox.ha.resource.status` | 1 | State of an HA-managed guest (`proxmox.ha.resource` = service id such as `vm:100`, plus `proxmox.vmid`), one series per `proxmox.ha.resource.state` (`started`, `stopped`, `disabled`, `ignored`, `error`, `fence`, `freeze`, `migrate`, `relocate`, `recovery`, `request_stop`): 1 for the current state, 0 for the others |
| `proxmox.ha.quorum.status` | 1 | Cluster quorum, one series per `proxmox.ha.quorum.state` (`ok`, `lost`) |
| `proxmox.replication.job.status` | 1 | Storage replication job (`proxmox.replication.job`, `proxmox.vmid`, `proxmox.replication.target`), one series per `proxmox.replication.job.state` (`ok`, `failed`); failed when the job has a non-zero fail count or an error |
| `proxmox.replication.job.age` | s | Seconds since the last successful synchronisation of a replication job; absent when it never synced |
| `proxmox.task.count` | {task} | Tasks that failed since the previous collection, per node and `proxmox.task.type` (`proxmox.task.state` = `failed`); each also becomes an ERROR log record |

## Operational notes

- Create an API token in Proxmox at **Datacenter → Permissions → API Tokens**. Grant it `PVEAuditor` role on `/` for read-only cluster-wide monitoring.
- The `endpoint` must use `https://`. The API certificate is verified by default; a self-signed Proxmox certificate needs `verify_tls: false`, or better, a proper certificate for production.
- Both QEMU VMs and LXC containers are monitored; they are distinguished by the `proxmox.vmid` tag.
- Disk, ZFS, backup and task series need `Sys.Audit` on `/nodes/<node>` (`PVEAuditor` on `/` covers it); a token without it simply contributes nothing for that endpoint. Backup job coverage reads `/cluster/backup`: a job with `all` covers every guest except its exclude list, otherwise its VM list and pool members, and a disabled job covers nothing.
- HA series read `/cluster/ha/status/current` and need `Sys.Audit` on `/`; replication series read `/nodes/<node>/replication` and need `Sys.Audit` on `/nodes/<node>` (both covered by `PVEAuditor` on `/`). A cluster without HA resources or replication jobs emits no HA resource or replication series and logs no warning. An HA resource in a state PVE does not document is skipped.

## Metric reference

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.proxmox.up` | `senhub.proxmox.up` | Proxmox API Up | # | 1 when the Proxmox REST API answered successfully, 0 on any connection or authentication failure |
| `proxmox.node.cpu.utilization` | `proxmox.node.cpu.utilization` | Proxmox Node {proxmox.node} CPU | % | CPU utilization of the Proxmox node in percent (0–100) |
| `proxmox.node.memory.used` | `proxmox.node.memory.used` | Proxmox Node {proxmox.node} Memory Used | B | Bytes of memory currently used on the Proxmox node |
| `proxmox.node.memory.limit` | `proxmox.node.memory.total` | Proxmox Node {proxmox.node} Memory Total | B | Total installed memory on the Proxmox node |
| `proxmox.node.status` | `proxmox.node.status` | Proxmox Node {proxmox.node} Status | # | 1 when the node is online, 0 otherwise |
| `proxmox.vm.cpu.utilization` | `proxmox.vm.cpu.utilization` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) CPU | % | CPU utilization of the VM or LXC container in percent (0–100) |
| `proxmox.vm.memory.used` | `proxmox.vm.memory.used` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Memory Used | B | Bytes of memory currently used by the VM or container |
| `proxmox.vm.memory.limit` | `proxmox.vm.memory.total` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Memory Total | B | Total memory allocated to the VM or container |
| `proxmox.vm.disk.read` | `proxmox.vm.disk.read` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Disk Read | B | Cumulative bytes read from disk by the VM or container since last boot |
| `proxmox.vm.disk.write` | `proxmox.vm.disk.write` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Disk Write | B | Cumulative bytes written to disk by the VM or container since last boot |
| `proxmox.vm.network.in` | `proxmox.vm.network.in` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Network In | B | Cumulative bytes received on all virtual NICs of the VM or container since last boot |
| `proxmox.vm.network.out` | `proxmox.vm.network.out` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Network Out | B | Cumulative bytes transmitted on all virtual NICs of the VM or container since last boot |
| `proxmox.vm.status` | `proxmox.vm.status` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Status | # | 1 when the VM or container is running, 0 otherwise |
| `proxmox.storage.used` | `proxmox.storage.used` | Proxmox Storage {proxmox.storage} Used | B | Bytes used on the Proxmox storage pool |
| `proxmox.storage.limit` | `proxmox.storage.total` | Proxmox Storage {proxmox.storage} Total | B | Total capacity of the Proxmox storage pool |
| `hw.physical_disk.status` | `hw.physical_disk.status` | Proxmox Disk {hw.name} Status {hw.state} | # | SMART state of the physical disk, one series per hw.state (ok, degraded, failed, predicted_failure): 1 for the current state, 0 for the others. PVE PASSED or OK maps to ok, FAILED to failed; a disk with no SMART verdict emits nothing. hw.id is the disk serial number, or the device path when PVE reports no serial |
| `hw.physical_disk.endurance_utilization` | `hw.physical_disk.endurance_utilization` | Proxmox Disk {hw.name} Life Remaining | # | Fraction of the SSD life remaining (1 new, 0 worn out), the PVE wearout percentage divided by 100 and not inverted; absent when PVE reports N/A |
| `hw.logical_disk.status` | `hw.logical_disk.status` | Proxmox ZFS Pool {hw.id} Status {hw.state} | # | State of the ZFS pool, one series per hw.state (ok, degraded, failed): 1 for the current state, 0 for the others. ONLINE maps to ok, DEGRADED to degraded, and FAULTED, UNAVAIL, REMOVED, SUSPENDED and OFFLINE to failed |
| `hw.logical_disk.limit` | `hw.logical_disk.limit` | Proxmox ZFS Pool {hw.id} Size | B | Total size of the ZFS pool |
| `hw.logical_disk.usage` | `hw.logical_disk.usage` | Proxmox ZFS Pool {hw.id} Bytes {hw.logical_disk.state} | B | Bytes of the ZFS pool allocated (used) and free |
| `hw.logical_disk.utilization` | `hw.logical_disk.utilization` | Proxmox ZFS Pool {hw.id} Utilization | # | Fraction of the ZFS pool allocated (used over size, 0 to 1) |
| `proxmox.zfs.pool.fragmentation` | `proxmox.zfs.pool.fragmentation` | Proxmox ZFS Pool {hw.id} Fragmentation | # | Fragmentation of the ZFS pool free space as a ratio (0 to 1), the PVE percentage divided by 100 |
| `proxmox.vm.backup.age` | `proxmox.vm.backup.age` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Last Backup Age | s | Seconds since the last successful backup of the VM or container, from the newest of its vzdump task history and the backup archives listed on the node storages; absent when no backup is known |
| `proxmox.vm.backup.status` | `proxmox.vm.backup.status` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Last Backup {proxmox.vm.backup.state} | # | Outcome of the last finished vzdump task naming this guest, one series per proxmox.vm.backup.state (ok, failed): 1 for the current state, 0 for the other. Warnings count as ok; absent when no such task is in the history (a scheduled job covering several guests names none) |
| `proxmox.storage.status` | `proxmox.storage.status` | Proxmox Storage {proxmox.storage} {proxmox.storage.state} | # | Availability of the storage from the node, one series per proxmox.storage.state (active, inactive): 1 for the current state, 0 for the other. An unreachable PBS or NFS storage is inactive |
| `proxmox.vm.backup.unprotected.count` | `proxmox.vm.backup.unprotected.count` | Proxmox Guests Without Backup | # | Guests covered by no enabled backup job of /cluster/backup: a job with all set covers every guest except its exclude list, otherwise its vmid list and its pool members; a disabled job covers nothing |
| `proxmox.backup.job.count` | `proxmox.backup.job.count` | Proxmox Backup Jobs | # | Number of enabled backup jobs in /cluster/backup |
| `proxmox.task.count` | `proxmox.task.count` | Proxmox Failed Tasks {proxmox.task.type} | # | Tasks of this type that ended with a status other than OK since the previous collection (the first cycle counts the last interval only), per node; each one is also published as an ERROR log record carrying the tail of its task log |
| `proxmox.ha.resource.status` | `proxmox.ha.resource.status` | Proxmox HA Resource {proxmox.ha.resource} {proxmox.ha.resource.state} | # | State of an HA-managed guest from /cluster/ha/status/current, one series per proxmox.ha.resource.state (started, stopped, disabled, ignored, error, fence, freeze, migrate, relocate, recovery, request_stop): 1 for the current state, 0 for the others. A resource in a state PVE does not document produces nothing; absent when HA is not configured |
| `proxmox.ha.quorum.status` | `proxmox.ha.quorum.status` | Proxmox HA Quorum {proxmox.ha.quorum.state} | # | Cluster quorum as reported by /cluster/ha/status/current, one series per proxmox.ha.quorum.state (ok, lost): 1 for the current state, 0 for the other |
| `proxmox.replication.job.status` | `proxmox.replication.job.status` | Proxmox Replication Job {proxmox.replication.job} {proxmox.replication.job.state} | # | Outcome of a storage replication job of the node, one series per proxmox.replication.job.state (ok, failed): failed when the job has a non-zero fail count or an error, 1 for the current state, 0 for the other. Absent when no replication is configured |
| `proxmox.replication.job.age` | `proxmox.replication.job.age` | Proxmox Replication Job {proxmox.replication.job} Last Sync Age | s | Seconds since the last successful synchronisation of the replication job; absent when the job has never synced |
| `senhub.probe.check.result` | `senhub.probe.check.result` | Check Result | # | Why the last check failed: 0 ok, 1 target down, 2 auth denied, 3 timeout, 4 TLS error, 5 config error, 6 protocol error, 7 permission denied |

<!-- schema:metrics:end -->
