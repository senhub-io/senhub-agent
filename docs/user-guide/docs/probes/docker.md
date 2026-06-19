!!! info
    **License: Free** — part of the universal collection tier.

# Docker

The `docker` probe monitors Docker containers via the Docker Engine API Unix
socket, reporting per-container CPU, memory, network I/O, block I/O and
running state. No external client library is required.

## Quick start

```yaml
probes:
  - name: docker
    type: docker
```

The probe connects to the local Docker socket at `/var/run/docker.sock` by
default. No parameters are required for a local setup.

## Parameters

This probe takes no configuration parameters — it connects to the local Docker
Engine socket automatically.

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

## Operational notes

- The agent must have read access to `/var/run/docker.sock`. On Linux, add the agent user to the `docker` group or run as root.
- All containers visible to the socket are monitored; use Docker labels or external filtering to restrict if needed.
- Stopped containers emit `senhub.docker.up=0` without CPU/memory metrics.
