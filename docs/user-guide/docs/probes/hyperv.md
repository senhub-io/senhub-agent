<img src="https://api.iconify.design/devicon/hyperv.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Hyper-V

The `hyperv` probe monitors Hyper-V virtual machines on the local Windows
Server host via WMI (`root\virtualization\v2`), reporting per-VM CPU usage,
memory assignment and running state.

**Windows Server only.** The probe does not start on Linux or macOS.

## Quick start

```yaml
probes:
  - name: hyperv
    type: hyperv
```

No parameters are required — the probe reads WMI on the local host automatically.

## Parameters

This probe takes no configuration parameters.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.hyperv.up` | 1 | 1 when the Hyper-V WMI namespace is reachable |
| `hyperv.vm.cpu.usage` | 1 | CPU utilization ratio (0–1) per virtual machine, tagged with `hyperv.vm.name` |
| `hyperv.vm.memory.assigned` | By | Memory currently assigned to the VM |
| `hyperv.vm.memory.demand` | By | Memory the VM is actively demanding |
| `hyperv.vm.state` | 1 | VM running state: 1 = Running, 0 = not running, tagged with `state` |

## Operational notes

- The agent must run with administrator privileges — the Hyper-V WMI namespace is access-controlled.
- One set of metrics per discovered VM; the `hyperv.vm.name` tag carries the VM's display name.
- The `state` tag on `hyperv.vm.state` carries the raw Hyper-V enabled-state string for informational display.
