<img src="../../assets/probe-logos/smart.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# S.M.A.R.T. Disk Health

The `smart` probe monitors local disk health via `smartctl` (smartmontools),
covering SATA/SAS drives (ATA S.M.A.R.T. attributes) and NVMe drives
(NVMe health information log). Each drive is reported separately via the
`smart.device` tag.

Requires `smartmontools` to be installed on the machine.

## Quick start

```yaml
# probes.d/10-smart.yaml — each file under probes.d/ is a YAML array of probes
- name: smart
  type: smart
```

No parameters are required — the probe auto-discovers all drives visible to
`smartctl`.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `devices` | No | - | Device paths to poll; empty means smartctl --scan. Example: `/dev/sda, /dev/nvme0` |
| `exclude_devices` | No | - | Device paths to skip from the scan, matched exactly |
| `smartctl_path` | No | `smartctl` | Path of the smartctl binary when it is not on PATH |
| `use_sudo` | No | `false` | Prefix every smartctl call with sudo (Unix) |
| `interval` | No | `300` | Seconds between collections |
| `exec_timeout` | No | `10` | Seconds one smartctl call may take |

<!-- schema:params:end -->

`devices` and `exclude_devices` take the paths `smartctl --scan` prints. On a
host with many drives, listing the ones you care about is cheaper than
scanning: each drive costs one `smartctl` invocation per cycle.

`use_sudo` is for a packaged agent that does not run as root and has a
sudoers rule for `smartctl`.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `smart.disk.health` | 1 | 1 when S.M.A.R.T. overall assessment passed, 0 when failed, tagged with `smart.device` |
| `smart.disk.reallocated_sectors` | {sector} | Reallocated sector count (SATA/SAS) — non-zero indicates drive degradation |
| `smart.disk.power_on_hours` | h | Cumulative power-on hours |
| `smart.disk.temperature` | Cel | Drive temperature |
| `smart.nvme.media_errors` | # | Cumulative media and data integrity errors |
| `smart.nvme.available_spare` | % | NVMe available spare capacity percentage |
| `smart.nvme.percentage_used` | % | NVMe lifetime wear indicator |
| `smart.nvme.data_units_read` | By | Total data read from the NVMe drive |
| `smart.nvme.data_units_written` | By | Total data written to the NVMe drive |

## Operational notes

- The agent must run as `root` to allow `smartctl` to access drive hardware directly.
- Install smartmontools: `apt install smartmontools` or `yum install smartmontools`.
- Not all drives expose all attributes. The probe silently omits metrics for attributes the drive does not report.

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
| `senhub.smart.up` | `senhub.smart.up` | S.M.A.R.T. Up | # | 1 when at least one disk could be read, 0 when none could |
| `senhub.smart.state` | `senhub.smart.state` | S.M.A.R.T. State ({reason}) | # | one-hot over ok / no_devices / devices_unreadable / partially_readable — says WHY up has its value, so a host with no disks is distinguishable from one whose disks the agent cannot open |
| `senhub.smart.devices.found` | `senhub.smart.devices.found` | S.M.A.R.T. Devices Found | # | Devices the scan reported, whether or not they could be read |
| `senhub.smart.devices.readable` | `senhub.smart.devices.readable` | S.M.A.R.T. Devices Readable | # | Devices the agent could actually query; below devices.found means a permission or hardware problem |
| `smart.disk.health` | `smart.disk.health` | S.M.A.R.T. Health {smart.device} | # | 1 when the S.M.A.R.T. overall assessment PASSED, 0 when FAILED |
| `smart.disk.reallocated_sectors` | `smart.disk.reallocated_sectors` | Reallocated Sectors {smart.device} | # | Count of sectors remapped to spare area (attribute 5); >0 signals media degradation |
| `smart.disk.pending_sectors` | `smart.disk.pending_sectors` | Pending Sectors {smart.device} | # | Sectors waiting to be remapped (attribute 197); >0 indicates unstable sectors |
| `smart.disk.uncorrectable_errors` | `smart.disk.uncorrectable_errors` | Uncorrectable Errors {smart.device} | # | Offline uncorrectable sectors (attribute 198); persistent read failures |
| `smart.disk.power_on_hours` | `smart.disk.power_on_hours` | Power-On Hours {smart.device} | h | Total drive power-on time (attribute 9) |
| `smart.disk.temperature` | `smart.disk.temperature` | Temperature {smart.device} | °C | Drive temperature in degrees Celsius (attribute 194 or NVMe temperature log) |
| `smart.disk.read_error_rate` | `smart.disk.read_error_rate` | Read Error Rate {smart.device} | # | Raw read error rate (attribute 1); high values indicate head or platter issues |
| `smart.nvme.available_spare` | `smart.nvme.available_spare` | NVMe Available Spare {smart.device} | % | Percentage of spare capacity remaining (0-100; <threshold triggers warning) |
| `smart.nvme.percentage_used` | `smart.nvme.percentage_used` | NVMe Wear Level {smart.device} | % | Wear indicator: percentage of rated lifetime consumed (0-100) |
| `smart.nvme.data_units_read` | `smart.nvme.data_units_read` | NVMe Data Units Read {smart.device} | # | Total number of 512-byte data units read (one unit = 1000 × 512 B on most controllers) |
| `smart.nvme.data_units_written` | `smart.nvme.data_units_written` | NVMe Data Units Written {smart.device} | # | Total number of 512-byte data units written |
| `smart.nvme.media_errors` | `smart.nvme.media_errors` | NVMe Media Errors {smart.device} | # | Cumulative media and data integrity errors |
| `smart.nvme.temperature` | `smart.nvme.temperature` | NVMe Temperature {smart.device} | °C | NVMe controller/composite temperature in degrees Celsius |

<!-- schema:metrics:end -->
