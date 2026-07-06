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

| Parameter | Default | Description |
|---|---|---|
| `host` | required | IP address or hostname of the Modbus TCP device |
| `port` | `502` | Modbus TCP port |
| `unit_id` | `1` | Modbus unit (slave) ID |
| `registers` | required | List of register definitions (see below) |

### Register definition

| Field | Description |
|---|---|
| `name` | Register name — becomes the `register.name` tag and PRTG channel name |
| `address` | Modicon 1-based holding-register address (e.g. 40001) |
| `type` | Data type: `uint16`, `int16`, `uint32`, `int32`, `float32_abcd`, `float32_cdab` |
| `scale` | Multiplier applied after decoding (default `1.0`) |
| `unit` | OTel unit string for the value (e.g. `Cel`, `%`, `1`) |
| `description` | Human-readable label |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `modbus.register.value` | configurable | Decoded register value, tagged with `register.name` and `register.address` |
| `modbus.up` | 1 | 1 when the Modbus TCP device answered all register reads this cycle |

## Operational notes

- `host` is required; the probe will fail to start without it.
- Address numbering follows the Modicon 1-based convention (40001 = holding register 0). Subtract 40001 to get the 0-based Modbus Protocol Data Unit address if needed.
- For `float32_abcd` vs `float32_cdab`, check your device manual for the byte-word order it uses.
