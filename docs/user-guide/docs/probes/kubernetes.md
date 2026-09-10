<img src="https://cdn.simpleicons.org/kubernetes" alt="" class="probe-page-logo probe-page-logo-si">

!!! info
    **License: Free** — part of the universal collection tier.

# Kubernetes

The `kubernetes` probe monitors a Kubernetes cluster via the API server,
collecting node health and capacity, pod and container state, every workload
kind, storage, quotas and autoscaling — and publishing the cluster's own
Events on the log rail, which is where Kubernetes explains why a metric
moved.

## Quick start

**In-cluster** (agent runs as a Pod with a ServiceAccount):

```yaml
# probes.d/10-kubernetes.yaml — each file under probes.d/ is a YAML array of probes
- name: kubernetes
  type: kubernetes
```

**Out-of-cluster** (agent runs outside the cluster):

```yaml
# probes.d/10-kubernetes.yaml
- name: kubernetes
  type: kubernetes
  params:
    kubeconfig: /home/agent/.kube/config
```

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->

| Parameter | Required | Default | Description |
|---|---|---|---|
| `kubeconfig` | No | - | Path of a kubeconfig file; empty uses the in-cluster service account. One instance per cluster: the identity comes from the cluster, so two instances pointing at two clusters do not collide. Example: `/home/agent/.kube/config` |
| `interval` | No | `30` | Seconds between collections |
| `namespaces` | No | - | Namespace selection |
| `namespaces.include` | No | - | Namespaces to watch; empty means all |
| `namespaces.exclude` | No | `[kube-system]` | Namespaces to skip |
| `collect` | No | - | Resource kinds to collect |
| `collect.nodes` | No | `true` | Node readiness, capacity and pressure conditions |
| `collect.pods` | No | `true` | Pod phase, readiness, restarts and resource requests |
| `collect.containers` | No | `true` | Per-container state, waiting reason and resources |
| `collect.deployments` | No | `true` | Deployment replica health |
| `collect.statefulsets` | No | `true` | StatefulSet replica health |
| `collect.daemonsets` | No | `true` | DaemonSet scheduling health |
| `collect.replicasets` | No | `false` | ReplicaSet replicas; off because each Deployment revision keeps one |
| `collect.jobs` | No | `true` | Job active, succeeded and failed counts |
| `collect.cronjobs` | No | `true` | CronJob active jobs and suspended state |
| `collect.storage` | No | `true` | PersistentVolumes and claims |
| `collect.quotas` | No | `true` | ResourceQuota limits and use |
| `collect.autoscalers` | No | `true` | HorizontalPodAutoscaler replica counts |
| `collect.events` | No | `true` | Cluster events, published on the log rail |

<!-- schema:params:end -->

| Parameter | Default | Description |
|---|---|---|
| `kubeconfig` | — | Path to a kubeconfig file. When empty, the probe uses the in-cluster ServiceAccount token |
| `interval` | `30` | Seconds between collections |
| `namespaces.include` | all | List of namespaces to monitor |
| `namespaces.exclude` | `[kube-system]` | Namespaces to skip |
| `collect.nodes` | `true` | Node health, capacity and pressure conditions |
| `collect.pods` | `true` | Pod phase, readiness, restarts and resource requests |
| `collect.containers` | `true` | Per-container state, waiting reason and resources |
| `collect.deployments` | `true` | Deployment replica health |
| `collect.statefulsets` | `true` | StatefulSet replica health |
| `collect.daemonsets` | `true` | DaemonSet scheduling health |
| `collect.replicasets` | **`false`** | Off by default: a Deployment owns one ReplicaSet per revision, so they multiply series without adding a fact the Deployment does not carry. Turn on while chasing a stuck rollout, where the previous ReplicaSet staying non-zero is exactly the symptom |
| `collect.jobs` | `true` | Job active/succeeded/failed counts |
| `collect.cronjobs` | `true` | CronJob active jobs and suspended state |
| `collect.storage` | `true` | PersistentVolumes and Claims |
| `collect.quotas` | `true` | ResourceQuota hard limits and use |
| `collect.autoscalers` | `true` | HorizontalPodAutoscaler replica counts |
| `collect.events` | `true` | Cluster Events, published on the log rail |

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
| `k8s.node.condition.disk_pressure` | 1 | **1 means the pressure IS present** — the opposite polarity to `ready`. A node under disk pressure is still `Ready` right up to the moment it is not: the kubelet begins evicting pods while readiness stays true. Same shape for `memory_pressure`, `pid_pressure`, `network_unavailable` |
| `k8s.pod.cpu.request` / `.memory.request` | 1 / By | What the scheduler committed for this pod, summed over its containers. With the limits below, this is what answers "is this cluster over-committed" — a question that cannot be answered retroactively without the series |
| `k8s.pod.cpu.limit` / `.memory.limit` | 1 / By | The ceiling before throttling or an OOM kill. **Absent when no limit is set**: a pod with no limit is unbounded, which is a different fact from a limit of zero |
| `k8s.container.waiting` | 1 | 1 while the container waits, tagged with the reason. `ready=0` with rising restarts describes both CrashLoopBackOff and an image still pulling — the reason is what separates them, and they call for opposite reactions |
| `k8s.statefulset.*`, `k8s.daemonset.*`, `k8s.job.*`, `k8s.cronjob.*` | 1 | Desired against actual per workload, tagged `k8s.workload.name` / `k8s.workload.kind` |
| `k8s.persistentvolumeclaim.phase` | 1 | One series per phase. A claim stuck `Pending` is why the pod that wants it never starts — indistinguishable, from the pod's own metrics, from a cluster simply out of CPU |
| `k8s.resourcequota.hard` / `.used` | 1 | The pair is the point: a quota at 99 % is why a deployment will not scale, and neither number alone says so |
| `k8s.hpa.current_replicas` / `.max_replicas` | 1 | An autoscaler pinned at max is the cluster refusing to grow, invisible from the workload's own replica counts |

## Cluster Events

Events ride the **log rail**, not the metric rail: they are timestamped
sentences — `0/5 nodes are available: 5 Insufficient cpu`, `Failed to pull
image`, `Liveness probe failed` — and counting them would keep the number
while throwing away the diagnosis.

A Kubernetes `Warning` is recorded at **Error** severity. Kubernetes has only
two event types and no error level, so a failed mount and an OOM kill arrive
at the same level as routine notices; recording them as warnings buries them.

Each record carries the identity label its subject's metrics already carry —
an event about a pod carries `k8s.pod.name` — so you can go from "why did
this pod restart" to that pod's series without parsing the message.

Only new events are published. The API returns its whole retention window on
every call, and the first cycle only sets the cursor: replaying that window at
start would deliver an hour of past incidents as though they were happening
now.

## Operational notes

- For in-cluster operation, create a `ClusterRole` granting `get`/`list`/`watch` on `nodes`, `pods`, `deployments`, `statefulsets`, `daemonsets`, `replicasets`, `jobs`, `cronjobs`, `persistentvolumes`, `persistentvolumeclaims`, `resourcequotas`, `horizontalpodautoscalers` and `events`, and bind it to the agent's ServiceAccount. A missing permission disables that collector only — the others keep reporting, and the gap is logged rather than presenting as an empty cluster.
- Also grant `get` on the `kube-system` **namespace**. The cluster entity is keyed on that namespace's UID, which is the only identifier the cluster reports about itself. Without it the identity falls back to the API server address, which re-keys the cluster in the graph on any endpoint change and collides between clusters sharing an address.
- The `kube-system` namespace is excluded by default. Override with `namespaces.include` / `namespaces.exclude` as needed.
- Metric names follow the `k8s.*` OTel semantic conventions aligned with the OpenTelemetry Kubernetes specification.
