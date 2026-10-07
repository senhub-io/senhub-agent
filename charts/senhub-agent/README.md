# senhub-agent Helm chart

Runs the SenHub Agent in a Kubernetes cluster, as a DaemonSet (one agent
per node, the default) or one Deployment: probes to
PRTG, Nagios, Prometheus, Zabbix and OTLP, with optional read-only
monitoring of the cluster itself through the `kubernetes` probe.

User documentation: [Kubernetes (Helm)](../../docs/user-guide/docs/kubernetes-helm.md).

## Install

The chart is installed from this repository for now; publishing it to an
OCI registry is a later step.

```bash
git clone https://github.com/senhub-io/senhub-agent.git
kubectl create namespace senhub
# Host monitoring (the default) needs the privileged Pod Security level.
kubectl label namespace senhub pod-security.kubernetes.io/enforce=privileged
kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN='<token>'
helm install senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  --set secrets.existingSecret=senhub-agent-credentials \
  --set kubernetesProbe.enabled=true \
  --set rbac.kubernetesProbe.enabled=true
```

No licence is needed: the free tier collects everything but the Pro
probes. For those, add `SENHUB_LICENSE` to the Secret (a step you take
later, with `kubectl -n senhub patch secret` or by recreating it) and
restart the pod.

## Host monitoring

`hostMonitoring.enabled` (default `true`) makes the agent monitor the
node it runs on, not the container:

- `hostPID: true`, `hostNetwork: true`, `dnsPolicy: ClusterFirstWithHostNet`:
  processes, network interfaces and their counters are the node's;
- the node's `/` is mounted read-only at `/host`, with `HostToContainer`
  propagation so the node's submounts appear beneath it, and the disk
  probe lists them (`SENHUB_HOST_ROOT=/host`, mount points reported
  without the prefix, container-runtime and kubelet bind mounts left out);
- `HOST_ETC`, `HOST_VAR` and `HOST_RUN` (gopsutil's variables) point
  under `/host`, so the OS release, boot time and sessions are the
  node's.

Without it, a pod already reports the node's CPU and memory (`/proc` is
not isolated), but disks show the container's overlay, the network a
single `eth0` and the process list the pod's few processes: half a node.
`hostMonitoring.enabled=false` is the container scope; CPU and memory
stay node-wide, and the rest is the pod's.

Trade-offs, to be accepted knowingly:

- The pod shares the node's PID and network namespaces and can read the
  whole node filesystem (read-only). It still runs as uid 10001 with no
  capability and no privilege escalation, so it reads what is
  world-readable, not root-only files. The namespace must allow the
  `privileged` Pod Security level (the `baseline` level forbids hostPath
  and host namespaces).
- The pods use the node's network. In a DaemonSet the HTTP output
  listens on the node's loopback (`http.bind` empty) so that the agent
  API is not served in clear on every node address; the kubelet probes
  follow it. To open it, and turn TLS on, set `config.strategies.http`
  (`bind_address`, `tls.enabled: true`), see the user documentation.
  `http.port` must be free on the node.
- With host monitoring the workload is a **DaemonSet**: one agent per
  node, on the nodes it tolerates (not the control plane by default; see
  `tolerations`), updated one node at a time. `kind: Deployment` keeps
  one agent for the release (container scope, one node, remote targets).
- The host identity is the node's own `machine-id` (read under
  `/host/etc`). In a DaemonSet each node generates its own agent key at
  its first start and keeps it in a hostPath state directory
  (`daemonSet.stateHostPath`, default `/var/lib/senhub-agent`, chowned
  to uid 10001 by an init container); no identity Secret, no PVC, no
  Service, because a shared identity would merge the nodes into one
  agent. Each agent answers on `<node address>:http.port`.
- Not combinable with a DaemonSet (the chart refuses): the Kubernetes
  probe (it would run once per node: install a second, Deployment
  release), a ServiceMonitor, a whole `config.agent`, `identity.existingSecret`.

## What it creates

| Object | When | Why |
|---|---|---|
| DaemonSet, `RollingUpdate` (1 unavailable) | `kind` empty with host monitoring, or `DaemonSet` | One agent per node, own identity in the node's state directory |
| Deployment, 1 replica, `Recreate` | otherwise | The agent. Not horizontally scalable (see `values.yaml`) |
| Secret `<release>-identity` | unless `identity.existingSecret` | Host identity and agent key. Kept after `helm uninstall` (`helm.sh/resource-policy: keep`) |
| ConfigMap | always | Probe and output fragments, copied into `probes.d/` and `strategies.d/` by an init container |
| Secret `<release>-env` | `secrets.values` set | Variables for `${env:NAME}` references |
| ServiceAccount | `serviceAccount.create` | Token mounted only when the Kubernetes probe needs it |
| ClusterRole + ClusterRoleBinding | `rbac.kubernetesProbe.enabled` | Read-only access for the `kubernetes` probe |
| Service | `service.enabled` | The HTTP output: console, PRTG, Nagios, Prometheus |
| ServiceMonitor | `serviceMonitor.enabled` and the CRD present | Prometheus Operator scrape of `/metrics` |
| PersistentVolumeClaim `<release>-state` | `persistence.enabled` | State directory: log probe bookmarks. Kept after `helm uninstall` |

## Uninstall

DaemonSet: the per-node state (agent key, bookmarks) stays on each node
in `daemonSet.stateHostPath`. Remove it on every node to start over:
`sudo rm -rf /var/lib/senhub-agent`.

Deployment: `helm uninstall` leaves two objects on purpose: the identity Secret, so a
reinstall under the same name brings the same agent back, and the
`<release>-state` claim, so the log bookmarks are not lost. To remove
them as well:

```bash
helm uninstall senhub-agent -n senhub
kubectl -n senhub delete secret senhub-agent-identity
kubectl -n senhub delete pvc senhub-agent-state
```

(The names are `<release>-identity` and `<release>-state`; with a
`fullnameOverride`, `<fullname>-identity` and `<fullname>-state`.)

## Identity

The agent key (what PRTG, Nagios and a Prometheus scrape authenticate
with) and the host identity (`host.id`) are generated at first install
and kept in a Secret annotated `helm.sh/resource-policy: keep`. The pod
receives them as `SENHUB_AGENT_KEY` and `SENHUB_HOST_ID`, and the host
identity is also mounted as `/etc/machine-id`, where the agent reads it.

A Secret rather than a volume: it follows the pod to any node or zone,
needs no StorageClass, and survives the release. Reinstalling under the
same release name and namespace brings the same agent back.

`helm template` cannot read the cluster, so a tool that renders with it
(Argo CD) would generate a new identity at each render. With such a tool,
set `identity.hostId` and `identity.agentKey`, or `identity.existingSecret`.

## Configuration

Two ways, which combine:

- **`env`**: the `SENHUB_*` variables the image entrypoint turns into a
  configuration (OTLP endpoint, Zabbix server, tags). The entrypoint writes `agent.yaml`, the host probes and the HTTP
  output, and checks the result before the agent starts.
- **`config.probes` / `config.strategies`**: fragments written as
  `probes.d/60-<key>.yaml` and `strategies.d/60-<key>.yaml`. A probe
  fragment is a list of probes; a strategy fragment is the settings of
  one output named by its key. Either may also be given as a YAML
  string.

`config.agent` replaces the generated `agent.yaml` entirely: the
entrypoint then writes nothing and every `env` value is ignored. The
chart still sets `agent.key` to `${env:SENHUB_AGENT_KEY}` and adds the
HTTP output (without the console, which needs an administration key)
unless `config.strategies.http` is set.

Credentials never go in the ConfigMap. Put them in a Secret
(`secrets.existingSecret`, or `secrets.values` for a chart-managed one),
whose keys become environment variables, and reference them:
`password: "${env:DB_PASSWORD}"`.

The configuration is assembled once per pod. A change to the values
rolls the pod; a change to an existing Secret needs
`kubectl rollout restart`. Changes made from the web console last until
the pod is replaced.

## Kubernetes probe permissions

`rbac.kubernetesProbe.enabled` grants exactly what the probe calls
(`internal/agent/probes/kubernetes`), all read-only:

| API group | Resources | Verbs |
|---|---|---|
| core | `namespaces` | `get` (kube-system, the cluster identity), `list` |
| core | `nodes`, `pods`, `persistentvolumes`, `persistentvolumeclaims`, `resourcequotas`, `events` | `list` |
| apps | `deployments`, `statefulsets`, `daemonsets`, `replicasets` | `list` |
| batch | `jobs`, `cronjobs` | `list` |
| autoscaling | `horizontalpodautoscalers` | `list` |

No `watch`, no Secrets, no ConfigMaps, no write.

## Values

| Key | Default | Description |
|---|---|---|
| `edition` | `full` | `full` (licensed probes, `ghcr.io/senhub-io/senhub-agent`) or `oss` (`ghcr.io/senhub-io/senhub-agent-oss`) |
| `image.repository` | `""` | Overrides the repository `edition` picks |
| `image.tag` | `""` | Image tag; empty uses the chart `appVersion` (0.6.1). There is no `latest` |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | |
| `nameOverride`, `fullnameOverride` | `""` | |
| `hostname` | `""` | Pod host name, reported as the host's name; empty uses the release full name. Ignored with `hostMonitoring.enabled` (the node's name) |
| `kind` | `""` | `DaemonSet` or `Deployment`; empty = DaemonSet with host monitoring, else Deployment |
| `daemonSet.stateHostPath` | `/var/lib/senhub-agent` | Per-node state directory (hostPath, DaemonSet only) |
| `hostMonitoring.enabled` | `true` | Node scope: host PID and network namespaces, node `/` read-only at `hostMonitoring.hostRoot`. `false` = container scope |
| `hostMonitoring.hostRoot` | `/host` | Where the node's root filesystem is mounted |
| `logLevel` | `info` | `debug` starts the agent with `--verbose` |
| `logFilter` | `""` | With `debug`, `--filter` (module name prefix, e.g. `probe`) |
| `identity.existingSecret` | `""` | Secret holding `host-id` and `agent-key` |
| `identity.hostId` | `""` | 32 hexadecimal characters, dashes optional; generated when empty |
| `identity.agentKey` | `""` | A UUID; generated when empty |
| `env.otlpEndpoint` | `""` | `SENHUB_OTLP_ENDPOINT`, host:port; SenHub by default when `OTLP_BEARER_TOKEN` is set |
| `env.otlpProtocol` | `""` | `SENHUB_OTLP_PROTOCOL`: `grpc` or `http` |
| `env.otlpTLS` | `""` | `SENHUB_OTLP_TLS`: `false` for a plain-text collector |
| `env.entities` | `""` | `SENHUB_ENTITIES`: `false` to send no entities |
| `env.tags` | `{}` | `SENHUB_TAGS`, a map written as `k=v,k2=v2` |
| `env.zabbixServer` | `""` | `SENHUB_ZABBIX_SERVER`, host:port |
| `env.zabbixHostMetadata` | `""` | `SENHUB_ZABBIX_HOST_METADATA` |
| `env.timezone` | `""` | `TZ` |
| `secrets.existingSecret` | `""` | Secret whose keys become environment variables |
| `secrets.values` | `{}` | Keys and values of a chart-managed Secret, same use |
| `config.probes` | `{}` | Probe fragments, `probes.d/60-<key>.yaml` |
| `config.strategies` | `{}` | Output fragments, `strategies.d/60-<key>.yaml` |
| `config.agent` | `{}` | A whole `agent.yaml`; disables the `env` path |
| `http.port` | `8080` | Port of the HTTP output |
| `http.bind` | `""` | Address it listens on. Empty: `127.0.0.1` for a DaemonSet, `0.0.0.0` for a Deployment |
| `kubernetesProbe.enabled` | `false` | Adds a `kubernetes` probe for this cluster |
| `kubernetesProbe.name` | `cluster` | Its name |
| `kubernetesProbe.params` | `{}` | Its parameters (interval, namespaces, collect) |
| `serviceAccount.create` | `true` | |
| `serviceAccount.name` | `""` | |
| `serviceAccount.annotations` | `{}` | |
| `serviceAccount.automountToken` | `false` | Forced on by `rbac.kubernetesProbe.enabled` |
| `rbac.kubernetesProbe.enabled` | `false` | Read-only ClusterRole for the `kubernetes` probe |
| `persistence.enabled` | `false` | PVC for `/var/lib/senhub-agent` (emptyDir otherwise) |
| `persistence.existingClaim` | `""` | |
| `persistence.storageClass` | `""` | `-` for no class |
| `persistence.accessModes` | `[ReadWriteOnce]` | |
| `persistence.size` | `1Gi` | |
| `persistence.annotations` | `{}` | |
| `service.enabled` | `true` | |
| `service.type` | `ClusterIP` | |
| `service.port` | `8080` | |
| `service.annotations` | `{}` | |
| `serviceMonitor.enabled` | `false` | Needs the Prometheus Operator CRD |
| `serviceMonitor.interval` | `60s` | |
| `serviceMonitor.scrapeTimeout` | `30s` | |
| `serviceMonitor.labels` | `{}` | Labels your Prometheus selects ServiceMonitors by |
| `serviceMonitor.metricRelabelings`, `.relabelings` | `[]` | |
| `resources` | 50m / 96Mi requested, 384Mi limit | Raise the memory limit on a large cluster |
| `podSecurityContext` | uid/gid 10001, non-root, RuntimeDefault seccomp | |
| `securityContext` | read-only root, no privilege escalation, all capabilities dropped | |
| `startupProbe`, `livenessProbe`, `readinessProbe` | `GET /health` | `null` disables one |
| `extraEnv`, `extraVolumes`, `extraVolumeMounts` | `[]` | `extraEnv` can declare a probe with `SENHUB_PROBE_<NAME>_*`; a secret goes in `secrets` |
| `podAnnotations`, `podLabels`, `nodeSelector`, `tolerations`, `affinity` | empty | |
| `priorityClassName` | `""` | |
| `terminationGracePeriodSeconds` | `30` | |

## Checking the chart

```bash
make helm-lint
```

Lints the chart and renders it with the default values and every file
under `ci/`, without contacting a cluster.
