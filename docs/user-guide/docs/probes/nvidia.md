!!! info
    **License: Free** — part of the universal collection tier.

# NVIDIA GPU

The `nvidia` probe monitors NVIDIA GPUs on the local machine via `nvidia-smi`,
reporting utilization, memory usage, temperature, power draw, encoder/decoder
utilization and fan speed per detected GPU.

## Quick start

```yaml
probes:
  - name: nvidia
    type: nvidia
```

No parameters are required. The probe auto-detects all GPUs visible to
`nvidia-smi`.

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `nvidia_smi_path` | `nvidia-smi` | Path to the `nvidia-smi` binary if not in PATH |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.nvidia.up` | 1 | 1 when `nvidia-smi` returned data for the GPU, 0 when absent or failed |
| `gpu.utilization` | 1 | GPU core utilization ratio (0–1), tagged with `gpu.index` / `gpu.name` |
| `gpu.memory.used` | By | GPU memory currently in use |
| `gpu.memory.total` | By | Total GPU memory |
| `gpu.temperature` | Cel | GPU die temperature |
| `gpu.power.draw` | W | Power draw in watts |
| `gpu.encoder.utilization` | 1 | Video encoder utilization ratio (0–1) |
| `gpu.decoder.utilization` | 1 | Video decoder utilization ratio (0–1) |
| `gpu.fan.speed` | 1 | Fan speed ratio (0–1), when supported by the GPU |

## Operational notes

- Requires the NVIDIA driver and `nvidia-smi` to be installed on the machine.
- Multiple GPUs are each reported separately, tagged with `gpu.index` (0-based) and `gpu.name`.
- If `nvidia-smi` is absent or fails, only `senhub.nvidia.up=0` is emitted per detected GPU slot.
