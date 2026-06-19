<img src="https://api.iconify.design/mdi/server.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# IPMI / BMC Sensors

The `ipmi` probe reads hardware sensor data from the local machine's Baseboard
Management Controller via `ipmitool`, reporting temperatures, fan speeds,
voltages and power supply status.

**Linux only.** Requires `ipmitool` and the OpenIPMI kernel driver (`ipmi_si`).

## Quick start

```yaml
probes:
  - name: ipmi
    type: ipmi
```

For local sensors, no parameters are needed. The probe calls `ipmitool sdr`
against the local BMC.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `mode` | `local` | `local` (host's own BMC) or `remote` (poll a remote BMC over LAN) |
| `remote_host` | — | IP or hostname of the remote BMC (required when `mode: remote`) |
| `remote_user` | — | IPMI username for remote access |
| `remote_password` | — | IPMI password for remote access |
| `remote_iface` | `lanplus` | IPMI LAN interface: `lanplus` (IPMI 2.0) or `lan` (IPMI 1.5) |
| `include_types` | all | Restrict to these sensor types (e.g. `[Temperature, Fan]`) |
| `exclude_names` | — | Regex patterns to skip specific sensor names |
| `ipmitool_path` | `ipmitool` | Path to the `ipmitool` binary if not in PATH |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `hardware.temperature` | Cel | Temperature per BMC sensor, tagged with `hardware.component` |
| `hardware.fan.speed` | RPM | Fan speed per sensor |
| `hardware.voltage` | V | Voltage per sensor |
| `hardware.status` | 1 | Sensor status: 1 = ok, 0 = critical/non-recoverable |

## Operational notes

- The agent must run as `root` on Linux for `ipmitool` to access the `/dev/ipmi0` device.
- Load the OpenIPMI driver before starting the agent: `modprobe ipmi_si && modprobe ipmi_devintf`.
- Sensor names (the `hardware.component` tag) come directly from `ipmitool sdr` output and vary by hardware vendor.
