<img src="https://api.iconify.design/mdi/chip.svg?color=%23666" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Modbus TCP

The `modbus` probe polls Modbus TCP Holding Registers on PLCs, industrial
sensors, and smart-building controllers. Each configured register becomes one
metric with a name and unit you define. Supports `uint16`, `int16`, `uint32`,
`int32`, `float32_abcd` and `float32_cdab` register types.

## Quick start

```yaml
# probes.d/20-modbus.yaml — each file under probes.d/ is a YAML array of probes
- name: modbus-plc
  type: modbus
  params:
    host: 192.168.1.100
    registers:
      - name: temperature
        address: 40001
        type: float32_abcd
        scale: 0.1
        unit: Cel
        description: Room temperature
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `host` | Yes | - | Device address or hostname. Example: `192.168.1.100` |
| `port` | No | `502` | Modbus TCP port |
| `unit_id` | No | `1` | Modbus unit (slave) identifier |
| `timeout` | No | `10s` | Per-read timeout, seconds or a duration |
| `interval` | No | `30` | Seconds between collections |
| `registers` | Yes | - | Registers to read and how to decode them |
| `registers[].name` | Yes | - | Register name, the register.name attribute of the value |
| `registers[].address` | Yes | - | Holding-register address, Modicon 1-based (40001) or 0-based on non-standard devices |
| `registers[].type` | Yes | - | How the raw bytes are decoded. One of `uint16`, `int16`, `uint32`, `int32`, `float32_abcd`, `float32_cdab` |
| `registers[].scale` | No | `1` | Multiplier applied after decoding; 0 counts as 1 |
| `registers[].unit` | No | `1` | OTel unit of the value. Example: `Cel` |
| `registers[].description` | No | - | Human-readable label for dashboards |

<!-- schema:params:end -->

A register's `name` is also the PRTG channel name of its value.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `modbus.register.value` | configurable | Decoded register value, tagged with `register.name` and `register.address` |
| `modbus.up` | 1 | 1 when the Modbus TCP device answered all register reads this cycle |

## Operational notes

- `host` is required; the probe will fail to start without it.
- Address numbering follows the Modicon 1-based convention (40001 = holding register 0). Subtract 40001 to get the 0-based Modbus Protocol Data Unit address if needed.
- For `float32_abcd` vs `float32_cdab`, check your device manual for the byte-word order it uses.
