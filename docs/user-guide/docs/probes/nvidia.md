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

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.nvidia.up` | `nvidia_up_{gpu.index}` | # | 1 when nvidia-smi returned data for this GPU, 0 when nvidia-smi is absent or failed |
| `gpu.utilization` | `gpu_utilization_{gpu.index}` | % | GPU core utilization in percent (0–100); share of time the GPU was busy over the last sample period |
| `gpu.memory.used` | `gpu_memory_used_{gpu.index}` | B | GPU framebuffer memory currently in use, in bytes |
| `gpu.memory.total` | `gpu_memory_total_{gpu.index}` | B | Total GPU framebuffer memory capacity, in bytes |
| `gpu.memory.utilization` | `gpu_memory_utilization_{gpu.index}` | % | GPU framebuffer memory utilization in percent (0–100); share of total framebuffer in use |
| `gpu.temperature` | `gpu_temperature_{gpu.index}` | °C | GPU die temperature in degrees Celsius |
| `gpu.power.usage` | `gpu_power_usage_{gpu.index}` | W | Current GPU power draw in watts (not emitted when nvidia-smi reports N/A) |
| `gpu.power.limit` | `gpu_power_limit_{gpu.index}` | W | GPU enforced power limit in watts (not emitted when nvidia-smi reports N/A) |
| `gpu.encoder.utilization` | `gpu_encoder_utilization_{gpu.index}` | % | GPU hardware video encoder utilization in percent (0–100) |
| `gpu.decoder.utilization` | `gpu_decoder_utilization_{gpu.index}` | % | GPU hardware video decoder utilization in percent (0–100) |
| `gpu.fan.speed` | `gpu_fan_speed_{gpu.index}` | % | GPU fan speed in percent (0–100); 0 when fan speed is not available |

<!-- schema:metrics:end -->
