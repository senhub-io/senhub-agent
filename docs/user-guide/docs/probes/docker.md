<img src="https://cdn.simpleicons.org/docker" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Default | Description |
|---|---|---|
| `socket_path` | per platform (see above) | Where the Docker Engine listens. A Unix socket path, or a Windows named pipe as `npipe://./pipe/<name>`. |
| `interval` | `60` | Seconds between collections |
| `timeout` | `10` | Engine request timeout in seconds |
| `include` | all | Container name patterns to keep (shell globs); empty keeps every container |
| `exclude` | none | Container name patterns to drop; evaluated after `include` |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.docker.up` | 1 | 1 when the container is in the running state, 0 otherwise |
| `container.restarts` | {restart} | Number of times the container has been restarted |
| `container.cpu.usage.total` | s | Total CPU time consumed by the container (monotonic) |
| `container.memory.usage` | By | Current memory usage (RSS) |
| `container.memory.limit` | By | Memory limit configured for the container |
| `container.network.io.received` | By | Bytes received on all network interfaces (monotonic) |
| `container.network.io.sent` | By | Bytes sent on all network interfaces (monotonic) |
| `container.blockio.read` | By | Bytes read from block devices (monotonic) |
| `container.blockio.write` | By | Bytes written to block devices (monotonic) |

Each metric is tagged with `container_name` and `container_id`; CPU metrics are
additionally tagged with `core`.

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
