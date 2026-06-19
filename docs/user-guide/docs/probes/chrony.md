!!! info
    **License: Free** — part of the universal collection tier.

# Chrony (NTP)

The `chrony` probe monitors NTP synchronisation health on the local machine via
`chronyc tracking`. It reports time offset, frequency offset, skew, root delay
and dispersion, stratum and leap status.

Works on Linux and macOS. Requires `chronyc` to be present in the PATH.

## Quick start

```yaml
probes:
  - name: chrony
    type: chrony
```

No parameters are required — the probe reads the local chrony daemon.

## Parameters

This probe takes no configuration parameters.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.chrony.up` | 1 | 1 when chronyc returned a valid tracking response, 0 when chronyc failed or is not installed |
| `ntp.time.offset` | s | Estimated error of the system clock relative to the NTP reference |
| `ntp.frequency.offset` | ppm | Rate at which the system clock gains or loses time (parts per million) |
| `ntp.skew` | ppm | Estimated frequency error of the clock (uncertainty band) |
| `ntp.root.delay` | s | Total round-trip delay to the reference clock source |
| `ntp.root.dispersion` | s | Maximum error of the local clock relative to the reference source |
| `ntp.stratum` | 1 | Stratum of the NTP reference (1 = GPS/atomic, 2 = primary, …) |
| `ntp.leap_status` | 1 | Encoded leap indicator: 0 = normal, 1 = insert second, 2 = delete second, 3 = not synchronised |

## Operational notes

- This probe is host-local. It reads the chrony daemon on the machine the agent runs on, not a remote NTP server.
- `ntp.time.offset` is signed: positive means the local clock is ahead, negative means it is behind.
- If `chronyc` is not installed, only `senhub.chrony.up=0` is emitted.
