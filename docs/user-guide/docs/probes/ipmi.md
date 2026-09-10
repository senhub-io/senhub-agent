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
# probes.d/10-ipmi.yaml — each file under probes.d/ is a YAML array of probes
- name: ipmi
  type: ipmi
```

For local sensors, no parameters are needed. The probe calls `ipmitool sdr`
against the local BMC.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `mode` | No | `local` | local reads the host's own BMC; remote polls a BMC over LAN. One of `local`, `remote` |
| `remote` | No | - | Remote BMC access, used with mode remote |
| `remote.host` | No | - | BMC address or hostname; required with mode remote |
| `remote.username` | No | - | IPMI user |
| `remote.password` | No | - | IPMI user's password. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `remote.interface` | No | `lanplus` | ipmitool interface; lanplus for IPMI 2.0, lan for IPMI 1.5 |
| `sensors` | No | - | Sensor selection |
| `sensors.include_types` | No | - | Only these sensor types; empty means all. Example: `Temperature, Fan` |
| `sensors.exclude_names` | No | - | Regular expressions of sensor names to skip |
| `ipmitool_path` | No | `ipmitool` | Path of the ipmitool binary when it is not on the PATH. Example: `/usr/bin/ipmitool` |
| `interval` | No | `60` | Seconds between collections |
| `exec_timeout` | No | `10` | Seconds an ipmitool run may take before it is killed |

<!-- schema:params:end -->

To poll a BMC over the network instead of the host's own, set `mode: remote`
and give the BMC under the `remote` block. Sensor filters live under the
`sensors` block:

```yaml
# probes.d/10-ipmi.yaml
- name: ipmi
  type: ipmi
  params:
    mode: remote
    remote:
      host: 10.0.0.50
      username: monitor
      password: ${secret:ipmi.password}   # OS secret store; inline plaintext is auto-sealed on install
      interface: lanplus                  # lan for an IPMI 1.5 BMC
    sensors:
      include_types: [Temperature, Fan]
      exclude_names: ["^PSU2.*"]
```

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
