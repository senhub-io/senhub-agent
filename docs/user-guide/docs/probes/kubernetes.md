<img src="https://cdn.simpleicons.org/kubernetes" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Kubernetes

The `kubernetes` probe monitors a Kubernetes cluster via the API server,
collecting node readiness and allocatable resources, pod phase and container
restart counts, deployment replica health, and namespace-level rollups.

## Quick start

**In-cluster** (agent runs as a Pod with a ServiceAccount):

```yaml
probes:
  - name: kubernetes
    type: kubernetes
```

**Out-of-cluster** (agent runs outside the cluster):

```yaml
probes:
  - name: kubernetes
    type: kubernetes
    params:
      kubeconfig: /home/agent/.kube/config
```

## Parameters

| Parameter | Default | Description |
|---|---|---|
| `kubeconfig` | — | Path to a kubeconfig file. When empty, the probe uses the in-cluster ServiceAccount token |
| `namespaces.include` | all | List of namespaces to monitor |
| `namespaces.exclude` | `[kube-system]` | Namespaces to skip |

## Metrics

| Metric | Unit | Description |
|---|---|---|
| `senhub.kubernetes.up` | 1 | 1 when the API server is reachable |
| `k8s.node.ready` | 1 | 1 when the node reports the Ready condition, tagged with `k8s.node.name` |
| `k8s.node.cpu.allocatable` | {cpu} | Allocatable CPU on the node |
| `k8s.node.memory.allocatable` | By | Allocatable memory on the node |
| `k8s.pod.phase` | 1 | 1 for each pod in each phase (Running/Pending/Succeeded/Failed/Unknown), tagged with `k8s.pod.name`/`k8s.namespace.name`/phase |
| `k8s.container.restarts` | {restart} | Container restart count, tagged with `k8s.container.name`/`k8s.pod.name` |
| `k8s.deployment.available` | {pod} | Available replicas per deployment, tagged with `k8s.deployment.name`/`k8s.namespace.name` |
| `k8s.deployment.desired` | {pod} | Desired replicas per deployment |

## Operational notes

- For in-cluster operation, create a `ClusterRole` granting `get`/`list`/`watch` on `nodes`, `pods`, `deployments` and bind it to the agent's ServiceAccount.
- The `kube-system` namespace is excluded by default. Override with `namespaces.include` / `namespaces.exclude` as needed.
- Metric names follow the `k8s.*` OTel semantic conventions aligned with the OpenTelemetry Kubernetes specification.
