<img src="https://api.iconify.design/mdi/pulse.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

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

| Parameter | Required | Default | Description |
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
| `smart.disk.nvme.critical_warning` | 1 | NVMe critical warning bits (0 = healthy) |
| `smart.disk.nvme.available_spare` | % | NVMe available spare capacity percentage |
| `smart.disk.nvme.percentage_used` | % | NVMe lifetime wear indicator |
| `smart.disk.nvme.data_units_read` | By | Total data read from the NVMe drive |
| `smart.disk.nvme.data_units_written` | By | Total data written to the NVMe drive |

## Operational notes

- The agent must run as `root` to allow `smartctl` to access drive hardware directly.
- Install smartmontools: `apt install smartmontools` or `yum install smartmontools`.
- Not all drives expose all attributes. The probe silently omits metrics for attributes the drive does not report.
