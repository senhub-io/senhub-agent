# senhub-agent Helm chart

Runs the SenHub Agent in a Kubernetes cluster as one Deployment: probes to
PRTG, Nagios, Prometheus, Zabbix and OTLP, with optional read-only
monitoring of the cluster itself through the `kubernetes` probe.

User documentation: [Kubernetes (Helm)](../../docs/user-guide/docs/kubernetes-helm.md).

## Install

The chart is installed from this repository for now; publishing it to an
OCI registry is a later step.

```bash
git clone https://github.com/senhub-io/senhub-agent.git
kubectl create namespace senhub
kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN='<token>' \
  --from-literal=SENHUB_LICENSE='<licence JWT>'
helm install senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  --set secrets.existingSecret=senhub-agent-credentials \
  --set kubernetesProbe.enabled=true \
  --set rbac.kubernetesProbe.enabled=true
```

## What it creates

| Object | When | Why |
|---|---|---|
| Deployment, 1 replica, `Recreate` | always | The agent. Not horizontally scalable (see `values.yaml`) |
| Secret `<release>-identity` | unless `identity.existingSecret` | Host identity and agent key, kept across restarts and after `helm uninstall` |
| ConfigMap | always | Probe and output fragments, copied into `probes.d/` and `strategies.d/` by an init container |
| Secret `<release>-env` | `secrets.values` set | Variables for `${env:NAME}` references |
| ServiceAccount | `serviceAccount.create` | Token mounted only when the Kubernetes probe needs it |
| ClusterRole + ClusterRoleBinding | `rbac.kubernetesProbe.enabled` | Read-only access for the `kubernetes` probe |
| Service | `service.enabled` | The HTTP output: console, PRTG, Nagios, Prometheus |
| ServiceMonitor | `serviceMonitor.enabled` and the CRD present | Prometheus Operator scrape of `/metrics` |
| PersistentVolumeClaim | `persistence.enabled` | State directory: log probe bookmarks |

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
  configuration (OTLP endpoint, Zabbix server, tags, Azure Container
  Apps). The entrypoint writes `agent.yaml`, the host probes and the HTTP
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
| `image.tag` | `""` | Image tag; empty uses the chart `appVersion`. There is no `latest` |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | |
| `nameOverride`, `fullnameOverride` | `""` | |
| `hostname` | `""` | Pod host name, reported as the host's name; empty uses the release full name |
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
| `env.azure.app`, `.tenantId`, `.clientId`, `.subscriptionId`, `.resourceGroup` | `""` | `SENHUB_AZURE_*`; the client secret goes in `secrets` |
| `env.timezone` | `""` | `TZ` |
| `secrets.existingSecret` | `""` | Secret whose keys become environment variables |
| `secrets.values` | `{}` | Keys and values of a chart-managed Secret, same use |
| `config.probes` | `{}` | Probe fragments, `probes.d/60-<key>.yaml` |
| `config.strategies` | `{}` | Output fragments, `strategies.d/60-<key>.yaml` |
| `config.agent` | `{}` | A whole `agent.yaml`; disables the `env` path |
| `http.port` | `8080` | Port of the HTTP output |
| `http.bind` | `0.0.0.0` | Address it listens on |
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
| `extraEnv`, `extraVolumes`, `extraVolumeMounts` | `[]` | |
| `podAnnotations`, `podLabels`, `nodeSelector`, `tolerations`, `affinity` | empty | |
| `priorityClassName` | `""` | |
| `terminationGracePeriodSeconds` | `30` | |

## Checking the chart

```bash
make helm-lint
```

Lints the chart and renders it with the default values and every file
under `ci/`, without contacting a cluster.
