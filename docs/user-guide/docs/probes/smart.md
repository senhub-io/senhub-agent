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

| Parameter | Default | Description |
|---|---|---|
| `include_types` | all | Restrict to these sensor/drive types (e.g. `[ata, nvme]`) |
| `exclude_names` | — | Regex patterns to skip drives by device name (e.g. `/dev/sda`) |
| `smartctl_path` | `smartctl` | Path to the `smartctl` binary if not in PATH |

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
