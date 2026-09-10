<img src="https://cdn.simpleicons.org/proxmox" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Proxmox VE

The `proxmox` probe monitors a Proxmox VE cluster via the REST API, reporting
per-node CPU and memory utilization and status, per-VM (QEMU) and LXC container
CPU, memory, disk I/O, network throughput and running state, and storage pool
usage.

## Quick start

```yaml
# probes.d/10-proxmox.yaml — each file under probes.d/ is a YAML array of probes
- name: proxmox
  type: proxmox
  params:
    endpoint: https://pve.example.com:8006
    token_id: monitor@pve!agent
    token_secret: ${secret:proxmox.token_secret}   # OS secret store; inline plaintext is auto-sealed on install
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `endpoint` | Yes | - | HTTPS base URL of the cluster API. Example: `https://pve.example.com:8006` |
| `token_id` | Yes | - | API token identifier as user@realm!tokenname. Example: `monitor@pve!agent` |
| `token_secret` | Yes | - | API token secret. A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file |
| `verify_tls` | No | `true` | Verify the API certificate; false accepts a self-signed one |
| `node` | No | - | Only this node is collected; empty means every node of the cluster |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `15` | Request timeout in seconds |
| `instance_name` | No | - | Stable identity override for this cluster |

<!-- schema:params:end -->

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.proxmox.up` | 1 | 1 when the Proxmox API answered successfully |
| `proxmox.node.cpu.utilization` | 1 | Node CPU utilization ratio (0–1), tagged with `proxmox.node` |
| `proxmox.node.memory.used` | By | Node memory in use |
| `proxmox.node.memory.total` | By | Node total memory |
| `proxmox.node.status` | 1 | Node online state: 1 = online, 0 = offline |
| `proxmox.vm.cpu.utilization` | 1 | VM CPU utilization, tagged with `proxmox.vmid` / `proxmox.vm.name` |
| `proxmox.vm.memory.used` | By | VM memory used |
| `proxmox.vm.disk.read` | By | VM block I/O bytes read (monotonic) |
| `proxmox.vm.disk.write` | By | VM block I/O bytes written |
| `proxmox.vm.network.in` | By | VM network bytes received |
| `proxmox.vm.network.out` | By | VM network bytes transmitted |
| `proxmox.vm.status` | 1 | VM state: 1 = running, 0 = stopped/other |
| `proxmox.storage.used` | By | Storage pool space used, tagged with `proxmox.storage` |
| `proxmox.storage.total` | By | Storage pool total capacity |

## Operational notes

- Create an API token in Proxmox at **Datacenter → Permissions → API Tokens**. Grant it `PVEAuditor` role on `/` for read-only cluster-wide monitoring.
- The `endpoint` must use `https://`. The API certificate is verified by default; a self-signed Proxmox certificate needs `verify_tls: false`, or better, a proper certificate for production.
- Both QEMU VMs and LXC containers are monitored; they are distinguished by the `proxmox.vmid` tag.
