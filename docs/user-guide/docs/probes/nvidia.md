<img src="../../assets/probe-logos/nvidia.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# NVIDIA GPU

The `nvidia` probe monitors NVIDIA GPUs on the local machine via `nvidia-smi`,
reporting utilization, memory usage, temperature, power draw, encoder/decoder
utilization and fan speed per detected GPU.

## Quick start

```yaml
# probes.d/20-nvidia.yaml — each file under probes.d/ is a YAML array of probes
- name: nvidia
  type: nvidia
```

No parameters are required. The probe auto-detects all GPUs visible to
`nvidia-smi`.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:b5bfab08476a695a617c536ca46f96aa8580ebeec8f9c6623c7ed5a7a5a8fefb -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `nvidia_smi_path` | No | `nvidia-smi` | Path of the nvidia-smi binary when it is not on the PATH. Example: `/usr/bin/nvidia-smi` |
| `gpus` | No | - | GPU indices to report, as nvidia-smi numbers them; empty means all. Example: `0, 1` |
| `interval` | No | `30` | Seconds between collections |

<!-- schema:params:end -->

`gpus` is useful on a host where some cards belong to another team.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.nvidia.up` | 1 | 1 when `nvidia-smi` returned data for the GPU, 0 when absent or failed |
| `gpu.utilization` | 1 | GPU core utilization ratio (0–1), tagged with `gpu.index` / `gpu.name` |
| `gpu.memory.used` | By | GPU memory currently in use |
| `gpu.memory.total` | By | Total GPU memory |
| `gpu.temperature` | Cel | GPU die temperature |
| `gpu.power.usage` | W | Power draw in watts |
| `gpu.encoder.utilization` | 1 | Video encoder utilization ratio (0–1) |
| `gpu.decoder.utilization` | 1 | Video decoder utilization ratio (0–1) |
| `gpu.fan.speed` | 1 | Fan speed ratio (0–1), when supported by the GPU |

## Operational notes

- Requires the NVIDIA driver and `nvidia-smi` to be installed on the machine.
- Multiple GPUs are each reported separately, tagged with `gpu.index` (0-based) and `gpu.name`.
- If `nvidia-smi` is absent or fails, only `senhub.nvidia.up=0` is emitted per detected GPU slot.

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
| `senhub.nvidia.up` | `senhub.nvidia.up` | GPU {gpu.index} ({gpu.name}) Availability | # | 1 when nvidia-smi returned data for this GPU, 0 when nvidia-smi is absent or failed |
| `gpu.utilization` | `gpu.utilization` | GPU {gpu.index} ({gpu.name}) Utilization | % | GPU core utilization in percent (0–100); share of time the GPU was busy over the last sample period |
| `gpu.memory.used` | `gpu.memory.used` | GPU {gpu.index} ({gpu.name}) Memory Used | B | GPU framebuffer memory currently in use, in bytes |
| `gpu.memory.total` | `gpu.memory.total` | GPU {gpu.index} ({gpu.name}) Memory Total | B | Total GPU framebuffer memory capacity, in bytes |
| `gpu.memory.utilization` | `gpu.memory.utilization` | GPU {gpu.index} ({gpu.name}) Memory Utilization | % | GPU framebuffer memory utilization in percent (0–100); share of total framebuffer in use |
| `gpu.temperature` | `gpu.temperature` | GPU {gpu.index} ({gpu.name}) Temperature | °C | GPU die temperature in degrees Celsius |
| `gpu.power.usage` | `gpu.power.usage` | GPU {gpu.index} ({gpu.name}) Power Draw | W | Current GPU power draw in watts (not emitted when nvidia-smi reports N/A) |
| `gpu.power.limit` | `gpu.power.limit` | GPU {gpu.index} ({gpu.name}) Power Limit | W | GPU enforced power limit in watts (not emitted when nvidia-smi reports N/A) |
| `gpu.encoder.utilization` | `gpu.encoder.utilization` | GPU {gpu.index} ({gpu.name}) Encoder Utilization | % | GPU hardware video encoder utilization in percent (0–100) |
| `gpu.decoder.utilization` | `gpu.decoder.utilization` | GPU {gpu.index} ({gpu.name}) Decoder Utilization | % | GPU hardware video decoder utilization in percent (0–100) |
| `gpu.fan.speed` | `gpu.fan.speed` | GPU {gpu.index} ({gpu.name}) Fan Speed | % | GPU fan speed in percent (0–100); 0 when fan speed is not available |

<!-- schema:metrics:end -->
