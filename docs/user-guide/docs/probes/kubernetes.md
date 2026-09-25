<img src="../../assets/probe-logos/kubernetes.svg" alt="" class="probe-page-logo probe-page-logo-si">

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

| Parameter | Must set | Default | Description |
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

Turn `collect.replicasets` on while chasing a stuck rollout: the previous
ReplicaSet staying non-zero is exactly the symptom, and the Deployment
alone does not show it.

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

## Metric reference

Every metric this probe can emit. The first column is the name the
OTLP and Prometheus outputs use, the second the channel the PRTG and
Nagios outputs carry.

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Channel | Unit | Description |
|---|---|---|---|
| `senhub.kubernetes.up` | `k8s_cluster_up` | # | 1 when at least one API list succeeded this cycle, 0 when the API server was unreachable or returned an error |
| `k8s.node.ready` | `k8s_node_{k8s.node.name}_ready` | # | 1 when the node reports Ready condition True, 0 otherwise |
| `k8s.node.cpu.allocatable` | `k8s_node_{k8s.node.name}_cpu_allocatable` | # | Number of allocatable CPU cores on the node |
| `k8s.node.memory.allocatable` | `k8s_node_{k8s.node.name}_memory_allocatable` | B | Allocatable memory on the node in bytes |
| `k8s.node.pods.capacity` | `k8s_node_{k8s.node.name}_pods_capacity` | # | Maximum number of pods the node can host |
| `k8s.node.pods.allocatable` | `k8s_node_{k8s.node.name}_pods_allocated` | # | Number of pods that can still be scheduled on the node |
| `k8s.pod.phase` | `k8s_pod_{k8s.namespace.name}_{k8s.pod.name}_phase` | # | 1 when the pod phase is Running, 0 otherwise |
| `k8s.pod.ready` | `k8s_pod_{k8s.namespace.name}_{k8s.pod.name}_ready` | # | 1 when the pod Ready condition is True |
| `k8s.pod.restarts` | `k8s_pod_{k8s.namespace.name}_{k8s.pod.name}_restarts` | # | Total container restart count for the pod |
| `k8s.container.ready` | `k8s_container_{k8s.namespace.name}_{k8s.pod.name}_{k8s.container.name}_ready` | # | 1 when the container is in a ready state |
| `k8s.container.restarts` | `k8s_container_{k8s.namespace.name}_{k8s.pod.name}_{k8s.container.name}_restarts` | # | Number of times the container has been restarted |
| `k8s.deployment.available` | `k8s_deployment_{k8s.namespace.name}_{k8s.deployment.name}_available` | # | Number of available (ready) replicas for the deployment |
| `k8s.deployment.desired` | `k8s_deployment_{k8s.namespace.name}_{k8s.deployment.name}_desired` | # | Desired replica count for the deployment (spec.replicas) |
| `k8s.deployment.ready` | `k8s_deployment_{k8s.namespace.name}_{k8s.deployment.name}_ready` | # | 1 when available replicas >= desired replicas, 0 otherwise |
| `k8s.node.condition.memory_pressure` | `k8s_node_{k8s.node.name}_memory_pressure` | # | 1 when the node reports the Memory Pressure condition True, 0 otherwise. Note the polarity is the opposite of k8s.node.ready: here 1 is the bad state |
| `k8s.node.condition.disk_pressure` | `k8s_node_{k8s.node.name}_disk_pressure` | # | 1 when the node reports the Disk Pressure condition True, 0 otherwise. Note the polarity is the opposite of k8s.node.ready: here 1 is the bad state |
| `k8s.node.condition.pid_pressure` | `k8s_node_{k8s.node.name}_pid_pressure` | # | 1 when the node reports the PID Pressure condition True, 0 otherwise. Note the polarity is the opposite of k8s.node.ready: here 1 is the bad state |
| `k8s.node.condition.network_unavailable` | `k8s_node_{k8s.node.name}_network_unavailable` | # | 1 when the node reports the Network Unavailable condition True, 0 otherwise. Note the polarity is the opposite of k8s.node.ready: here 1 is the bad state |
| `k8s.pod.cpu.request` | `k8s_pod_{k8s.pod.name}_cpu_request` | cores | CPU Request summed over the pod's containers, init containers excluded (they do not hold their reservation for the pod's life) |
| `k8s.pod.cpu.limit` | `k8s_pod_{k8s.pod.name}_cpu_limit` | cores | CPU Limit summed over the pod's containers, init containers excluded (they do not hold their reservation for the pod's life) |
| `k8s.pod.memory.request` | `k8s_pod_{k8s.pod.name}_memory_request` | bytes | Memory Request summed over the pod's containers, init containers excluded |
| `k8s.pod.memory.limit` | `k8s_pod_{k8s.pod.name}_memory_limit` | bytes | Memory Limit summed over the pod's containers, init containers excluded |
| `k8s.container.cpu.request` | `k8s_container_{k8s.container.name}_cpu_request` | cores | CPU Request declared by this container |
| `k8s.container.cpu.limit` | `k8s_container_{k8s.container.name}_cpu_limit` | cores | CPU Limit declared by this container |
| `k8s.container.memory.request` | `k8s_container_{k8s.container.name}_memory_request` | bytes | Memory Request declared by this container |
| `k8s.container.memory.limit` | `k8s_container_{k8s.container.name}_memory_limit` | bytes | Memory Limit declared by this container |
| `k8s.container.waiting` | `k8s_container_{k8s.container.name}_waiting` | # | 1 while the container is in a waiting state; the reason attribute distinguishes CrashLoopBackOff from ImagePullBackOff, which call for opposite reactions |
| `k8s.statefulset.desired` | `k8s_statefulset_{k8s.workload.name}_desired` | # | desired count for this statefulset |
| `k8s.statefulset.ready` | `k8s_statefulset_{k8s.workload.name}_ready` | # | ready count for this statefulset |
| `k8s.statefulset.current` | `k8s_statefulset_{k8s.workload.name}_current` | # | current count for this statefulset |
| `k8s.statefulset.updated` | `k8s_statefulset_{k8s.workload.name}_updated` | # | updated count for this statefulset |
| `k8s.daemonset.desired_scheduled` | `k8s_daemonset_{k8s.workload.name}_desired_scheduled` | # | desired scheduled count for this daemonset |
| `k8s.daemonset.current_scheduled` | `k8s_daemonset_{k8s.workload.name}_current_scheduled` | # | current scheduled count for this daemonset |
| `k8s.daemonset.ready` | `k8s_daemonset_{k8s.workload.name}_ready` | # | ready count for this daemonset |
| `k8s.daemonset.misscheduled` | `k8s_daemonset_{k8s.workload.name}_misscheduled` | # | misscheduled count for this daemonset |
| `k8s.replicaset.desired` | `k8s_replicaset_{k8s.workload.name}_desired` | # | desired count for this replicaset |
| `k8s.replicaset.ready` | `k8s_replicaset_{k8s.workload.name}_ready` | # | ready count for this replicaset |
| `k8s.replicaset.available` | `k8s_replicaset_{k8s.workload.name}_available` | # | available count for this replicaset |
| `k8s.job.active` | `k8s_job_{k8s.workload.name}_active` | # | active count for this job |
| `k8s.job.succeeded` | `k8s_job_{k8s.workload.name}_succeeded` | # | succeeded count for this job |
| `k8s.job.failed` | `k8s_job_{k8s.workload.name}_failed` | # | failed count for this job |
| `k8s.job.desired_completions` | `k8s_job_{k8s.workload.name}_desired_completions` | # | desired completions count for this job |
| `k8s.cronjob.active_jobs` | `k8s_cronjob_{k8s.workload.name}_active_jobs` | # | active jobs count for this cronjob |
| `k8s.cronjob.suspended` | `k8s_cronjob_{k8s.workload.name}_suspended` | # | suspended count for this cronjob |
| `k8s.persistentvolume.capacity` | `k8s_pv_{k8s.persistentvolume.name}_capacity` | bytes | Declared capacity of the persistent volume |
| `k8s.persistentvolume.phase` | `k8s_pv_{k8s.persistentvolume.name}_phase_{phase}` | # | 1 when the volume is in this lifecycle phase. One series per phase rather than an enum value, so an unknown phase lights none of them instead of mapping onto one |
| `k8s.persistentvolumeclaim.requested` | `k8s_pvc_{k8s.persistentvolumeclaim.name}_requested` | bytes | Storage the claim asks for |
| `k8s.persistentvolumeclaim.capacity` | `k8s_pvc_{k8s.persistentvolumeclaim.name}_capacity` | bytes | Storage actually granted; can exceed the request when the storage class rounds up, and is absent while the claim is Pending |
| `k8s.persistentvolumeclaim.phase` | `k8s_pvc_{k8s.persistentvolumeclaim.name}_phase_{phase}` | # | 1 when the claim is in this phase. A claim stuck Pending is why the pod that wants it never starts |
| `k8s.resourcequota.hard` | `k8s_quota_{k8s.resourcequota.name}_{k8s.resourcequota.resource}_hard` | # | The quota ceiling for this resource. CPU is in cores, memory and storage in bytes, counts are plain integers |
| `k8s.resourcequota.used` | `k8s_quota_{k8s.resourcequota.name}_{k8s.resourcequota.resource}_used` | # | Current use against the ceiling. A quota at 99 percent is why a deployment will not scale, and neither number alone says so |
| `k8s.hpa.current_replicas` | `k8s_hpa_{k8s.hpa.name}_current_replicas` | # | Current replica count. An autoscaler pinned at max is the cluster refusing to grow, invisible from the workload's own counts |
| `k8s.hpa.desired_replicas` | `k8s_hpa_{k8s.hpa.name}_desired_replicas` | # | Desired replica count. An autoscaler pinned at max is the cluster refusing to grow, invisible from the workload's own counts |
| `k8s.hpa.min_replicas` | `k8s_hpa_{k8s.hpa.name}_min_replicas` | # | Min replica count. An autoscaler pinned at max is the cluster refusing to grow, invisible from the workload's own counts |
| `k8s.hpa.max_replicas` | `k8s_hpa_{k8s.hpa.name}_max_replicas` | # | Max replica count. An autoscaler pinned at max is the cluster refusing to grow, invisible from the workload's own counts |

<!-- schema:metrics:end -->
