<img src="../../assets/probe-logos/chrony.svg" alt="" class="probe-page-logo probe-page-logo-mdi">

!!! info
    **License: Free** — part of the universal collection tier.

# Chrony (NTP)

The `chrony` probe monitors NTP synchronisation health on the local machine via
`chronyc tracking`. It reports time offset, frequency offset, skew, root delay
and dispersion, stratum and leap status.

Works on Linux and macOS. Requires `chronyc` to be present on the host.

It reports what the chrony daemon believes about the clock it steers. To measure
the clock against an independent reference — and to cover hosts that run
systemd-timesyncd, ntpd, the Windows Time service or no daemon at all — see the
[NTP (direct)](ntp.md) probe. The two are complementary; the comparison at the
bottom of that page says which question each one answers.

## Quick start

```yaml
# probes.d/10-chrony.yaml — each file under probes.d/ is a YAML array of probes
- name: chrony
  type: chrony
```

No parameters are required — the probe reads the local chrony daemon.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:1a4de44575a4fb04122a8044198f9fae3d0f3e9d390397732561cb868cdcd964 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `chronyc_path` | No | `chronyc` | Path of the chronyc binary when it is not on the service's PATH. Example: `/usr/bin/chronyc` |
| `interval` | No | `30` | Seconds between collections |

<!-- schema:params:end -->

A hardened service unit does not inherit an interactive shell's `PATH`, so when
the probe reports `not_installed` on a host that has chrony, an absolute
`chronyc_path` such as `/usr/bin/chronyc` is the usual fix.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.chrony.up` | 1 | 1 when chronyc returned a valid tracking response, 0 when chronyc failed or is not installed |
| `senhub.chrony.state` | 1 | One series per reason, exactly one of which is 1: `ok`, `not_installed`, `exec_failed`, `parse_failed` |
| `ntp.time.offset` | ms | Estimated error of the system clock relative to the NTP reference |
| `ntp.frequency.offset` | ppm | Rate at which the system clock gains or loses time (parts per million) |
| `ntp.skew` | ppm | Estimated frequency error of the clock (uncertainty band) |
| `ntp.root.delay` | ms | Total round-trip delay to the reference clock source |
| `ntp.root.dispersion` | ms | Maximum error of the local clock relative to the reference source |
| `ntp.stratum` | 1 | Stratum of the NTP reference (1 = GPS/atomic, 2 = primary, …) |
| `ntp.leap_status` | 1 | Encoded leap indicator: 0 = normal, 1 = insert second, 2 = delete second, 3 = not synchronised |

## Reading the numbers

| `ntp.time.offset` | What it means |
|---|---|
| under 10 ms | Normal for a synchronised host. Nothing to do. |
| 10 ms to 100 ms | Correlating logs or traces across hosts starts putting events in the wrong order. |
| 100 ms to 1 s | Worth alerting. chrony is running but not steering the clock effectively. |
| over 60 s | One-time passwords (TOTP) begin to fail. |
| over 5 minutes | Kerberos and Active Directory authentication fails outright. |

Two signals matter as much as the offset itself:

- **`ntp.leap_status` = 3** means chrony has lost its sources and is no longer
  synchronised. This is the strongest single indicator, because an
  unsynchronised chrony keeps reporting the last offset it knew: the number
  stays reassuring while the clock quietly drifts.
- **`ntp.skew` climbing over days** is an early warning. Skew is the uncertainty
  on the clock's drift rate, and it rises when a hardware clock or a
  virtualisation host starts misbehaving — usually well before the time itself
  moves far enough to break anything.

## Troubleshooting

`senhub.chrony.state` names the cause directly, so a host with no chrony is
distinguishable from a probe that cannot read the chrony it has.

| State | Meaning | What to do |
|---|---|---|
| `ok` | Tracking was read and parsed. | — |
| `not_installed` | `chronyc` was not found. | Either the host genuinely does not run chrony — in which case remove the probe or use [NTP (direct)](ntp.md) instead — or the binary is not on the service's `PATH`, which `chronyc_path` fixes. |
| `exec_failed` | `chronyc` ran but exited non-zero. | Usually chronyd is not running, or its command socket is not reachable. Check `systemctl status chronyd`. |
| `parse_failed` | Output was returned but could not be read. | Report it: the message names the field index and the offending value. |

## Operational notes

- This probe is host-local. It reads the chrony daemon on the machine the agent
  runs on, not a remote NTP server.
- `ntp.time.offset` is signed: positive means the local clock is ahead, negative
  means it is behind.
- When `chronyc` cannot be read, only `senhub.chrony.up=0` and the
  `senhub.chrony.state` series are emitted. No measurement is invented.

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
| `senhub.chrony.up` | `senhub.chrony.up` | Chrony NTP Up | # | 1 when chronyc returned a valid tracking response, 0 when chronyc failed or is not installed |
| `senhub.chrony.state` | `senhub.chrony.state` | Chrony State ({reason}) | # | one-hot over ok / not_installed / exec_failed / parse_failed — says WHY up has its value, so a host without chrony is distinguishable from a probe that cannot read the chrony it has |
| `ntp.time.offset` | `ntp.time.offset` | NTP Time Offset | ms | Estimated error of the system clock relative to the NTP reference (positive = fast, negative = slow) |
| `ntp.frequency.offset` | `ntp.frequency.offset` | NTP Frequency Offset | ppm | Rate at which the system clock gains or loses time relative to the reference (parts per million) |
| `ntp.skew` | `ntp.skew` | NTP Skew | ppm | Estimated error bound on the frequency error (ppm); a high skew means the clock rate is uncertain |
| `ntp.root.delay` | `ntp.root.delay` | NTP Root Delay | ms | Total round-trip delay to the stratum-1 reference clock |
| `ntp.root.dispersion` | `ntp.root.dispersion` | NTP Root Dispersion | ms | Maximum clock error due to dispersion between the host and the stratum-1 source |
| `ntp.stratum` | `ntp.stratum` | NTP Stratum | # | Stratum level of the NTP hierarchy (1 = directly connected to reference, 16 = unsynchronised) |
| `ntp.leap_status` | `ntp.leap_status` | NTP Leap Status | # | Leap-second status reported by chrony: 0=Normal, 1=Insert second, 2=Delete second, 3=Not synchronised |

<!-- schema:metrics:end -->
