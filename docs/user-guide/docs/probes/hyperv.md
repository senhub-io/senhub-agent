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
# probes.d/10-hyperv.yaml — each file under probes.d/ is a YAML array of probes
- name: hyperv
  type: hyperv
```

No parameter is required: the probe reads WMI on the local host. Only the
collection interval can be tuned.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `interval` | No | `60` | Seconds between collections |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.hyperv.up` | 1 | 1 when the Hyper-V WMI namespace is reachable |
| `hyperv.vm.cpu.usage` | % | CPU utilization (0 to 100) per virtual machine, tagged with `hyperv.vm.name` |
| `hyperv.vm.memory.usage` | By | Memory currently used by the VM, as reported by Hyper-V |
| `hyperv.vm.state` | 1 | 1 when the VM is running, 0 otherwise |
| `hyperv.vm.count` | {vm} | Number of VMs per state, tagged with `state` (`running`, `stopped`, `paused`) |

## Operational notes

- The agent must run with administrator privileges — the Hyper-V WMI namespace is access-controlled.
- One set of metrics per discovered VM; the `hyperv.vm.name` tag carries the VM's display name and the `vmid` tag its immutable identifier.
- Per-VM CPU and memory are read from the summary information Hyper-V keeps for each VM; a VM without it only reports its state.
