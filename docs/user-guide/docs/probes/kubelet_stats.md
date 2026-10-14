# Kubelet Stats

!!! info
    **License: Free** — part of the universal collection tier.

The `kubelet_stats` probe reads the kubelet Summary API of the node the
agent runs on. It reports CPU, memory, filesystem and network usage for
the node, its pods and their containers. The agent is expected to run as a
DaemonSet so each instance reports its own node.

## Quick start

The Helm chart sets it up in its DaemonSet (`kubeletStats.enabled`, on by
default). By hand, in a DaemonSet with the ServiceAccount token mounted and
`SENHUB_K8S_NODE_IP` set from `status.hostIP`:

```yaml
# probes.d/10-kubelet-stats.yaml
- name: kubelet_stats
  type: kubelet_stats
```

## Prerequisites

The DaemonSet pod must mount its ServiceAccount token and CA certificate at
the default Kubernetes locations:

- `/var/run/secrets/kubernetes.io/serviceaccount/token`
- `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`

The probe calls the kubelet directly; it does not use the API server proxy.
The ServiceAccount needs only `get` on the `nodes/stats` subresource:

```yaml
rules:
- apiGroups: [""]
  resources: ["nodes/stats"]
  verbs: ["get"]
```

Without `endpoint`, the probe calls port 10250 on `SENHUB_K8S_NODE_IP`
(from `status.hostIP`), or on `SENHUB_K8S_NODE_NAME` when only the name is
set; a node name is often not resolvable from a pod, so prefer the IP.

Most kubelets serve a self-signed certificate that the ServiceAccount CA
does not sign (kubeadm's default). The probe then fails with
`reason=connect` until you either enable kubelet serving certificates
signed by the cluster CA (`serverTLSBootstrap`) or set
`insecure_skip_verify: true`.

## Parameters

<!-- schema:params:start -->
<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->
<!-- sha256:b6b867cd31b5dbad0b97b642e800e94bf4c27ed46ed45fac24995e84e03ca135 -->

| Parameter | Must set | Default | Description |
|---|---|---|---|
| `endpoint` | No | - | Base URL of the kubelet. Empty uses port 10250 on $SENHUB_K8S_NODE_IP, else on $SENHUB_K8S_NODE_NAME. Example: `https://127.0.0.1:10250` |
| `insecure_skip_verify` | No | `false` | Skip TLS certificate verification. Kubelets often serve self-signed certificates; the proper fix is kubelet serving certificate rotation/approval, not enabling this permanently |
| `interval` | No | `30` | Seconds between collections |
| `include_namespaces` | No | - | Namespace glob patterns to include; empty means all |
| `exclude_namespaces` | No | - | Namespace glob patterns to skip |

<!-- schema:params:end -->

## Metrics

<!-- schema:metrics:start -->
<!-- Generated from the probe's definition. Run `make docs-metrics` after changing it. -->

| Metric | Name | PRTG channel | Unit | Description |
|---|---|---|---|---|
| `senhub.kubelet_stats.scrape_errors` | `senhub.kubelet_stats.scrape_errors` | Kubelet scrape errors {reason} | # | Kubelet summary scrapes that failed since the agent started, by reason (connect, auth, status, decode) |
| `senhub.kubelet_stats.pods_seen` | `senhub.kubelet_stats.pods_seen` | Kubelet pods seen | # | Number of pods on the node after namespace filters |
| `k8s.node.cpu.usage` | `k8s.node.cpu.usage` | Node {k8s.node.name} CPU Usage | cores | Node CPU usage in cores, derived from usageNanoCores |
| `k8s.node.memory.working_set` | `k8s.node.memory.working_set` | Node {k8s.node.name} Memory Working Set | bytes | Node memory working set in bytes |
| `k8s.node.memory.available` | `k8s.node.memory.available` | Node {k8s.node.name} Memory Available | bytes | Node memory available in bytes |
| `k8s.node.filesystem.usage` | `k8s.node.filesystem.usage` | Node {k8s.node.name} Filesystem Usage | bytes | Node filesystem used bytes |
| `k8s.node.filesystem.capacity` | `k8s.node.filesystem.capacity` | Node {k8s.node.name} Filesystem Capacity | bytes | Node filesystem capacity in bytes |
| `k8s.node.filesystem.available` | `k8s.node.filesystem.available` | Node {k8s.node.name} Filesystem Available | bytes | Node filesystem available bytes |
| `k8s.node.network.io` | `k8s.node.network.io` | Node {k8s.node.name} Network {direction} | bytes | Node network bytes on its default interface, tagged with direction receive or transmit |
| `k8s.pod.cpu.usage` | `k8s.pod.cpu.usage` | Pod {k8s.namespace.name}/{k8s.pod.name} CPU Usage | cores | Pod CPU usage in cores, derived from usageNanoCores |
| `k8s.pod.memory.working_set` | `k8s.pod.memory.working_set` | Pod {k8s.namespace.name}/{k8s.pod.name} Memory Working Set | bytes | Pod memory working set in bytes |
| `k8s.pod.network.io` | `k8s.pod.network.io` | Pod {k8s.namespace.name}/{k8s.pod.name} Network {direction} | bytes | Pod network bytes, tagged with direction receive or transmit |
| `container.cpu.usage` | `container.cpu.usage` | Container {k8s.namespace.name}/{k8s.pod.name}/{k8s.container.name} CPU Usage | cores | Container CPU usage in cores, derived from usageNanoCores |
| `container.memory.working_set` | `container.memory.working_set` | Container {k8s.namespace.name}/{k8s.pod.name}/{k8s.container.name} Memory Working Set | bytes | Container memory working set in bytes |

<!-- schema:metrics:end -->

## What it does not do

- It does **not** report volume usage: one series per volume and pod is a
  high cardinality for little diagnostic value.
- It does **not** report container restarts. The kubelet Summary API does
  not expose restart counts.
- It does **not** report memory or CPU limits or utilisation ratios. Limits
  are not available in the Summary API.
- It does **not** report per-container network traffic.
- It does **not** query the API server or the metrics-server: the kubelet
  checks the token itself.
