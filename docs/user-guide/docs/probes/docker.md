<img src="../../assets/probe-logos/docker.svg" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Docker

The `docker` probe monitors Docker containers via the Docker Engine API Unix
socket, reporting per-container CPU, memory, network I/O, block I/O and
running state. No external client library is required.

## Quick start

```yaml
# probes.d/10-docker.yaml — each file under probes.d/ is a YAML array of probes
- name: docker
  type: docker
```

The probe connects to the local Docker Engine on whichever transport the
platform offers, with no parameter needed for a local setup:

| Platform | Default address | Transport |
|---|---|---|
| Linux, macOS | `/var/run/docker.sock` | Unix socket |
| Windows | `npipe://./pipe/docker_engine` | named pipe |

Docker Engine and Docker Desktop on Windows expose the API over a named pipe
rather than a socket, which is why the default differs. Set `socket_path`
to override it; a Windows pipe may be written `npipe://./pipe/<name>` or
`\\.\pipe\<name>`.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `socket_path` | No | - | Engine socket; /var/run/docker.sock on Unix, npipe://./pipe/docker_engine on Windows by default |
| `interval` | No | `60` | Seconds between collections |
| `timeout` | No | `10` | Engine request timeout in seconds |
| `include` | No | - | Container name patterns to keep; empty means all. Example: `web-*` |
| `exclude` | No | - | Container name patterns to drop; wins over include |

<!-- schema:params:end -->

`include` and `exclude` are shell-style globs (`web-*`, `db?`) matched against
the container's primary name.

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.docker.up` | 1 | 1 when the container is in the running state, 0 otherwise |
| `container.restarts` | {restart} | Number of times the container has been restarted |
| `container.cpu.usage.total` | s | Total CPU time consumed by the container (monotonic) |
| `container.memory.usage` | By | Current memory usage (RSS) |
| `senhub.docker.memory.limit` | By | Memory limit configured for the container |
| `container.network.io.usage.rx_bytes` | By | Bytes received on all network interfaces (monotonic) |
| `container.network.io.usage.tx_bytes` | By | Bytes sent on all network interfaces (monotonic) |
| `container.blockio.io_service_bytes_recursive.read` | By | Bytes read from block devices (monotonic) |
| `container.blockio.io_service_bytes_recursive.write` | By | Bytes written to block devices (monotonic) |

Each metric is tagged with `container_name` and `container_id`; CPU metrics are
additionally tagged with `core`.

## What a Windows container reports

The engine answers a different shape on Windows, so a few channels come
from different fields there and a few are absent:

| Channel | Linux | Windows |
|---|---|---|
| Memory usage | cgroup usage, minus the page cache for the working set | private working set, plus the commit charge and its peak |
| Memory limit | the cgroup limit | not reported by the engine, so zero |
| CPU percentage | container time against host time | container time against the wall time between two samples, times the processors the container may use |
| Block I/O | `blkio` counters per device | the storage counters the engine reports for the container |
| Per-core CPU, throttling, page-cache breakdown | reported | not reported by the engine |

A value the engine does not send is left at zero rather than guessed.

## Running without access to the Docker socket

`/var/run/docker.sock` is `root:docker` mode `0660`, so an agent running as the
unprivileged `senhub` service user cannot open it. The usual remedy is to add
that user to the `docker` group — and it is worth knowing what that grants:
anyone who can reach the socket can start a container that mounts the host
filesystem, so **`docker` group membership is root on the host by another
route**. It is a decision to make deliberately, which is why the installer never
makes it for you.

The probe does not require it. When the socket is unavailable it reads container
counters straight from cgroups, which are world-readable:

| Available from cgroups | Needs the socket |
|---|---|
| `container.cpu.*` | `container.network.*` |
| `container.memory.*` | `container.restarts` |
| `container.blockio.*` | container name, image, labels, state |
| `container.pids.count` | the container entity in topology |

That is 26 of the probe's 31 metrics with no group, no capability and no path to
root. Containers are identified by id alone in this mode — a name guessed from a
cgroup path would be wrong the moment someone renamed a container, and wrong
identity is worse than none.

**Per-container network counters are the real loss.** They live in the
container's network namespace rather than its cgroup, so there is no
unprivileged file to read; they are absent rather than reported as zero.

`senhub.docker.source` tells you which mode a cycle used, `socket` or `cgroup`.
Alert on it if you rely on the socket-only metrics — the two modes do not carry
the same fields, and a host that quietly fell back would otherwise look like a
host whose containers stopped using the network.

**Limitations:** the fallback reads cgroup **v2** only. On a cgroup v1 host the
probe still needs the socket, and says so in the log rather than publishing half
the picture.

### Keeping the metadata without granting host root

A **docker socket proxy** — a small HAProxy in front of the socket that
allow-lists only the endpoints the agent reads and returns `403` for everything
else — gives the container names and labels back without granting container
creation. The probe uses exactly two read-only endpoints,
`GET /containers/json` and `GET /containers/{id}/stats`, so the allow-list is
short. [Tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy)
is the usual implementation.

## Operational notes

- The agent must have read access to `/var/run/docker.sock`. On Linux, add the agent user to the `docker` group or run as root.
- All containers visible to the socket are monitored; use Docker labels or external filtering to restrict if needed. In cgroup mode every container with a cgroup is reported, and `include`/`exclude` cannot filter by name because no name is available.
- Stopped containers emit `senhub.docker.up=0` without CPU/memory metrics.

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.docker.source` | `docker_source_{source}` | # | one-hot over socket / cgroup — which source produced this cycle. The cgroup fallback works without the docker group but reports no per-container network counters, restart counts or container names |
| `senhub.docker.up` | `docker_{container_name}_up` | # | 1 when the container is running, 0 otherwise |
| `container.restarts` | `docker_{container_name}_restarts` | # | Number of times the container has been restarted |
| `container.cpu.usage.total` | `docker_{container_name}_cpu_total` | ns | Cumulative CPU time consumed by the container |
| `container.cpu.usage.kernelmode` | `docker_{container_name}_cpu_kernelmode` | ns | Cumulative CPU time consumed in kernel mode by the container |
| `container.cpu.usage.usermode` | `docker_{container_name}_cpu_usermode` | ns | Cumulative CPU time consumed in user mode by the container |
| `senhub.docker.cpu.system` | `docker_{container_name}_cpu_system` | ns | System CPU time since the container started (used to compute CPU %) |
| `senhub.docker.cpu.online` | `docker_{container_name}_cpu_online` | # | Number of online CPUs available to the container |
| `senhub.docker.cpu.percent` | `docker_{container_name}_cpu_percent` | % | CPU utilisation percentage (same formula as docker stats) |
| `container.cpu.usage.percpu` | `docker_{container_name}_cpu_core{core}` | ns | Cumulative CPU time consumed by the container on a specific core (percpu_usage) |
| `container.cpu.throttling_data.throttled_periods` | `docker_{container_name}_cpu_throttled_periods` | # | Number of periods where the container was throttled (from throttling_data) |
| `container.cpu.throttling_data.periods` | `docker_{container_name}_cpu_throttling_periods` | # | Total number of CPU periods (from throttling_data.throttling_periods) |
| `container.cpu.throttling_data.throttled_time` | `docker_{container_name}_cpu_throttled_time` | ns | Cumulative nanoseconds the container was throttled (from throttling_data) |
| `container.memory.usage` | `docker_{container_name}_memory` | B | Current memory usage of the container (includes page cache) |
| `senhub.docker.memory.limit` | `docker_{container_name}_memory_limit` | B | Memory limit configured for the container (0 = unlimited) |
| `container.memory.rss` | `docker_{container_name}_memory_rss` | B | Anonymous memory (RSS on cgroupsv1, anon on cgroupsv2) |
| `container.memory.cache` | `docker_{container_name}_memory_cache` | B | Page cache memory (cache on cgroupsv1, file on cgroupsv2) |
| `container.memory.swap` | `docker_{container_name}_memory_swap` | B | Swap memory usage |
| `senhub.docker.memory.working_set` | `docker_{container_name}_memory_working_set` | B | Working set memory (usage minus page cache) — what docker stats reports as MEM USAGE |
| `container.memory.anon` | `docker_{container_name}_memory_anon` | B | Anonymous (non-file-backed) memory; uses rss key on cgroupsv1, anon on cgroupsv2 |
| `container.memory.mapped_file` | `docker_{container_name}_memory_mapped_file` | B | Memory mapped to files (cgroupsv1 mapped_file; absent on cgroupsv2) |
| `container.memory.pgfault` | `docker_{container_name}_memory_pgfault` | # | Cumulative minor page faults (pgfault from memory_stats.stats) |
| `container.memory.pgmajfault` | `docker_{container_name}_memory_pgmajfault` | # | Cumulative major page faults requiring disk I/O (pgmajfault from memory_stats.stats) |
| `container.memory.unevictable` | `docker_{container_name}_memory_unevictable` | B | Memory that cannot be reclaimed (locked pages, mlocked regions) |
| `container.memory.writeback` | `docker_{container_name}_memory_writeback` | B | Memory queued for write-back to disk |
| `container.memory.hierarchical_memory_limit` | `docker_{container_name}_memory_hierarchical_limit` | B | Memory limit of the container's cgroup hierarchy (cgroupsv1 only) |
| `container.memory.active_anon` | `docker_{container_name}_memory_active_anon` | B | Recently-accessed anonymous memory in the active LRU list |
| `container.memory.inactive_anon` | `docker_{container_name}_memory_inactive_anon` | B | Less-recently-accessed anonymous memory in the inactive LRU list |
| `container.memory.active_file` | `docker_{container_name}_memory_active_file` | B | Recently-accessed file-backed memory in the active LRU list |
| `container.memory.inactive_file` | `docker_{container_name}_memory_inactive_file` | B | Less-recently-accessed file-backed memory in the inactive LRU list (used for working-set on cgroupsv2) |
| `container.pids.count` | `docker_{container_name}_pids` | # | Number of processes running inside the container |
| `senhub.docker.pids.limit` | `docker_{container_name}_pids_limit` | # | Maximum number of processes allowed in the container (0 = unlimited) |
| `container.network.io.usage.tx_bytes` | `docker_{container_name}_net_tx` | B | Cumulative bytes transmitted across all network interfaces |
| `container.network.io.usage.rx_bytes` | `docker_{container_name}_net_rx` | B | Cumulative bytes received across all network interfaces |
| `senhub.docker.network.tx_packets` | `docker_{container_name}_net_tx_packets` | # | Cumulative packets transmitted across all network interfaces |
| `senhub.docker.network.rx_packets` | `docker_{container_name}_net_rx_packets` | # | Cumulative packets received across all network interfaces |
| `container.network.io.usage.tx_errors` | `docker_{container_name}_net_tx_errors` | # | Cumulative transmit errors across all network interfaces |
| `container.network.io.usage.rx_errors` | `docker_{container_name}_net_rx_errors` | # | Cumulative receive errors across all network interfaces |
| `senhub.docker.network.tx_dropped` | `docker_{container_name}_net_tx_dropped` | # | Cumulative transmitted packets dropped across all network interfaces |
| `senhub.docker.network.rx_dropped` | `docker_{container_name}_net_rx_dropped` | # | Cumulative received packets dropped across all network interfaces |
| `container.blockio.usage.total` | `docker_{container_name}_blkio` | B | Cumulative block I/O bytes (read + write, or Total entry when present) |
| `container.blockio.io_service_bytes_recursive.read` | `docker_{container_name}_blkio_read` | B | Cumulative block I/O read bytes |
| `container.blockio.io_service_bytes_recursive.write` | `docker_{container_name}_blkio_write` | B | Cumulative block I/O write bytes |
| `senhub.docker.blkio.service_time.total` | `docker_{container_name}_blkio_svc_time` | ns | Cumulative block I/O service time in nanoseconds (io_service_time_recursive Total; cgroupsv1 only) |
| `senhub.docker.blkio.sectors.total` | `docker_{container_name}_blkio_sectors` | # | Cumulative block I/O sectors transferred (io_sectors_recursive Total; cgroupsv1 only) |

<!-- schema:metrics:end -->
