# Next (unreleased)

Nothing released yet since 0.6.5. Changes land here as they are merged.

<div class="rn-filter"></div>

## Before you upgrade

- **`proxmox`: Ceph, PBS, quorum, HA and replication are preview and off by default.** The probe is validated on a single Proxmox VE node (QEMU guests, LXC containers, ZFS and directory storages). Cluster quorum, high availability, storage replication, Ceph-backed storages and Proxmox Backup Server storages are in preview: not validated in a real environment, and each is switched on explicitly with `enable_cluster`, `enable_ha`, `enable_replication`, `enable_ceph` and `enable_pbs`. Left off, a part makes no API call and produces no series, so the `rbd`, `cephfs` and `pbs` storages and the HA, quorum and replication series seen with earlier builds disappear until you enable them. See [Proxmox VE](../probes/proxmox.md).

## Process probe: `top_n` reports a per-name roll-up

With `filter.top_n`, the `process` probe now ranks process names and reports the per-name roll-up of the top, without `process.pid` and `process.creation.time` on the series. Short-lived processes no longer create new series each time they enter the top. Per-process series under `top_n` remain available with `detail.per_pid: true`, which `agent config check` flags with a cardinality warning.
