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
<!-- sha256:ab29f8261ab0433230e24ee32a418dca0590d9edcc788f1231254cb1c561d9ca -->

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

Every metric this probe can emit. **Metric** is the OpenTelemetry name the
OTLP, Prometheus and Zabbix outputs derive theirs from. **Name** is what a
[Nagios check](../nagios.md) and the API `metrics=` filter match.
**PRTG channel** is the label PRTG shows, placeholders filled from the
series' tags.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.proxmox.up` | `senhub.proxmox.up` | Proxmox API Up | # | 1 when the Proxmox REST API answered successfully, 0 on any connection or authentication failure |
| `proxmox.node.cpu.utilization` | `proxmox.node.cpu.utilization` | Proxmox Node {proxmox.node} CPU | % | CPU utilization of the Proxmox node in percent (0–100) |
| `proxmox.node.memory.used` | `proxmox.node.memory.used` | Proxmox Node {proxmox.node} Memory Used | B | Bytes of memory currently used on the Proxmox node |
| `proxmox.node.memory.total` | `proxmox.node.memory.total` | Proxmox Node {proxmox.node} Memory Total | B | Total installed memory on the Proxmox node |
| `proxmox.node.status` | `proxmox.node.status` | Proxmox Node {proxmox.node} Status | # | 1 when the node is online, 0 otherwise |
| `proxmox.vm.cpu.utilization` | `proxmox.vm.cpu.utilization` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) CPU | % | CPU utilization of the VM or LXC container in percent (0–100) |
| `proxmox.vm.memory.used` | `proxmox.vm.memory.used` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Memory Used | B | Bytes of memory currently used by the VM or container |
| `proxmox.vm.memory.total` | `proxmox.vm.memory.total` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Memory Total | B | Total memory allocated to the VM or container |
| `proxmox.vm.disk.read` | `proxmox.vm.disk.read` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Disk Read | B | Cumulative bytes read from disk by the VM or container since last boot |
| `proxmox.vm.disk.write` | `proxmox.vm.disk.write` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Disk Write | B | Cumulative bytes written to disk by the VM or container since last boot |
| `proxmox.vm.network.in` | `proxmox.vm.network.in` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Network In | B | Cumulative bytes received on all virtual NICs of the VM or container since last boot |
| `proxmox.vm.network.out` | `proxmox.vm.network.out` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Network Out | B | Cumulative bytes transmitted on all virtual NICs of the VM or container since last boot |
| `proxmox.vm.status` | `proxmox.vm.status` | Proxmox VM {proxmox.vm.name} ({proxmox.vmid}) Status | # | 1 when the VM or container is running, 0 otherwise |
| `proxmox.storage.used` | `proxmox.storage.used` | Proxmox Storage {proxmox.storage} Used | B | Bytes used on the Proxmox storage pool |
| `proxmox.storage.total` | `proxmox.storage.total` | Proxmox Storage {proxmox.storage} Total | B | Total capacity of the Proxmox storage pool |

<!-- schema:metrics:end -->
