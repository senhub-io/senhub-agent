<img src="../../assets/probe-logos/proxmox.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Must set | Default | Description |
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

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.proxmox.up` | `proxmox_up` | # | 1 when the Proxmox REST API answered successfully, 0 on any connection or authentication failure |
| `proxmox.node.cpu.utilization` | `node_{proxmox.node}_cpu` | % | CPU utilization of the Proxmox node in percent (0–100) |
| `proxmox.node.memory.used` | `node_{proxmox.node}_mem_used` | B | Bytes of memory currently used on the Proxmox node |
| `proxmox.node.memory.total` | `node_{proxmox.node}_mem_total` | B | Total installed memory on the Proxmox node |
| `proxmox.node.status` | `node_{proxmox.node}_status` | # | 1 when the node is online, 0 otherwise |
| `proxmox.vm.cpu.utilization` | `vm_{proxmox.vmid}_cpu` | % | CPU utilization of the VM or LXC container in percent (0–100) |
| `proxmox.vm.memory.used` | `vm_{proxmox.vmid}_mem_used` | B | Bytes of memory currently used by the VM or container |
| `proxmox.vm.memory.total` | `vm_{proxmox.vmid}_mem_total` | B | Total memory allocated to the VM or container |
| `proxmox.vm.disk.read` | `vm_{proxmox.vmid}_disk_read` | B | Cumulative bytes read from disk by the VM or container since last boot |
| `proxmox.vm.disk.write` | `vm_{proxmox.vmid}_disk_write` | B | Cumulative bytes written to disk by the VM or container since last boot |
| `proxmox.vm.network.in` | `vm_{proxmox.vmid}_net_in` | B | Cumulative bytes received on all virtual NICs of the VM or container since last boot |
| `proxmox.vm.network.out` | `vm_{proxmox.vmid}_net_out` | B | Cumulative bytes transmitted on all virtual NICs of the VM or container since last boot |
| `proxmox.vm.status` | `vm_{proxmox.vmid}_status` | # | 1 when the VM or container is running, 0 otherwise |
| `proxmox.storage.used` | `storage_{proxmox.storage}_used` | B | Bytes used on the Proxmox storage pool |
| `proxmox.storage.total` | `storage_{proxmox.storage}_total` | B | Total capacity of the Proxmox storage pool |

<!-- schema:metrics:end -->
