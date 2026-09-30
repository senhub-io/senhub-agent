# Kubernetes (Helm)

A Helm chart runs the agent in a Kubernetes cluster: one pod, the
[container image](container.md), a stable identity, and, when you ask
for it, read-only monitoring of the cluster itself through the
[`kubernetes` probe](probes/kubernetes.md).

The chart lives in the agent's repository under `charts/senhub-agent`
and is installed from there for now. Publishing it to an OCI registry,
so that `helm install oci://...` works without a clone, is a later step.

## Install

```bash
git clone https://github.com/senhub-io/senhub-agent.git
kubectl create namespace senhub

# Credentials live in a Secret; its keys become environment variables.
kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN='<your token>' \
  --from-literal=SENHUB_LICENSE='<licence JWT>'

helm install senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  --set secrets.existingSecret=senhub-agent-credentials
```

That is a first run: the agent watches the pod it runs in and pushes to
SenHub. Without a licence, choose the free build with
`--set edition=oss`.

Check it started, and read the lines the entrypoint writes about the
configuration it built:

```bash
kubectl -n senhub logs deploy/senhub-agent -c agent
kubectl -n senhub port-forward svc/senhub-agent 8080:8080
curl http://127.0.0.1:8080/health
```

## Monitoring the cluster

Two values turn it on: one adds the probe, the other the read-only
permissions it needs.

```bash
helm upgrade senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  --reuse-values \
  --set kubernetesProbe.enabled=true \
  --set rbac.kubernetesProbe.enabled=true
```

The ClusterRole grants what the probe calls and nothing else:

| API group | Resources | Verbs |
|---|---|---|
| core | `namespaces` | `get` (the `kube-system` namespace, whose UID identifies the cluster), `list` |
| core | `nodes`, `pods`, `persistentvolumes`, `persistentvolumeclaims`, `resourcequotas`, `events` | `list` |
| apps | `deployments`, `statefulsets`, `daemonsets`, `replicasets` | `list` |
| batch | `jobs`, `cronjobs` | `list` |
| autoscaling | `horizontalpodautoscalers` | `list` |

No `watch`, no write, and neither Secrets nor ConfigMaps. The service
account token is mounted only when this is on: nothing else in the agent
talks to the API server.

The probe's own settings go in `kubernetesProbe.params`, with the
parameters of the [probe page](probes/kubernetes.md):

```yaml
kubernetesProbe:
  enabled: true
  params:
    interval: 60
    namespaces:
      exclude: [kube-system, kube-public]
    collect:
      replicasets: false
```

## Configuring

Everything is a value; the full list is in the chart's `README.md` and
`values.yaml`. Three kinds of setting, which combine.

**The variables of the image.** `env` carries the settings the
[entrypoint](container.md#variables) turns into a configuration:

```yaml
env:
  otlpEndpoint: otel-collector.observability.svc:4317
  otlpTLS: false
  zabbixServer: zabbix-server.monitoring.svc:10051
  tags:
    environment: production
    cluster: paris-1
```

**Probes and outputs.** `config.probes` and `config.strategies` are
written into the configuration as `probes.d/60-<key>.yaml` and
`strategies.d/60-<key>.yaml` before the agent starts. A probe entry is a
list of probes, as a `probes.d` file holds; an output entry is the
settings of the output its key names:

```yaml
config:
  probes:
    databases:
      - name: orders-db
        type: postgresql
        params:
          host: orders-db.shop.svc
          username: monitor
          password: "${env:ORDERS_DB_PASSWORD}"
  strategies:
    zabbix:
      server: zabbix-server.monitoring.svc:10051
```

**Secrets.** A credential never goes in these values: they end up in a
ConfigMap. It goes in a Secret whose keys become environment variables,
and the configuration refers to it with `${env:NAME}`, which the agent
resolves at every start. Either name an existing Secret, as in the
install above, or let the chart create one from a values file you keep
out of version control:

```yaml
secrets:
  values:
    ORDERS_DB_PASSWORD: "..."
```

The configuration is built once for each pod. Changing a value rolls
the pod; changing an existing Secret does not, so restart it:
`kubectl -n senhub rollout restart deployment/senhub-agent`. A change
made from the web console lasts until the pod is replaced: the values
are the configuration.

**A whole configuration.** `config.agent` replaces the generated
`agent.yaml`. The entrypoint then writes nothing and every `env` value
is ignored, exactly as with a [mounted configuration](container.md#bringing-your-own-configuration).
The chart still points `agent.key` at the identity below and adds the
HTTP output on `http.port`, without the web console, which needs an
administration key you then provide in `config.strategies.http`.

## Identity

The agent key (what PRTG, Nagios and a Prometheus scrape read with, and
what tells two agents apart downstream) and the host identity are
generated at first install and kept in the Secret
`<release>-identity`. The pod receives them as `SENHUB_AGENT_KEY` and
`SENHUB_HOST_ID`, and the host identity is also mounted as
`/etc/machine-id`, where the agent reads it. A pod replaced, rescheduled
on another node or upgraded to a new image is the same host and the same
agent.

A Secret rather than a volume, because it follows the pod to any node
and any zone and needs no storage class. The Secret is kept when the
release is uninstalled, so reinstalling under the same name in the same
namespace brings the same agent back; delete it to start over.

Read the agent key, for PRTG or Nagios:

```bash
kubectl -n senhub get secret senhub-agent-identity \
  -o jsonpath='{.data.agent-key}' | base64 -d; echo
```

!!! warning "GitOps"
    Generation reads the Secret already in the cluster to keep its
    values. A tool that renders the chart with `helm template`, such as
    Argo CD, cannot read it and would mint a new identity at each sync.
    With such a tool, set `identity.hostId` (32 hexadecimal characters)
    and `identity.agentKey` (a UUID), or `identity.existingSecret`
    naming a Secret with the keys `host-id` and `agent-key`. One value
    per installation: two agents sharing an identity are one agent to
    everything downstream.

The host *name* is the pod's host name, set to the release name so it
does not change at every rollout. `hostname` sets another.

## Reading the agent

The Service `<release>` exposes the HTTP output on port 8080: PRTG,
Nagios, Prometheus and the console, at the same paths as on any host
([HTTP / HTTPS](http-https.md)). PRTG and Nagios outside the cluster
reach it through whatever you already use to expose a Service: a
`NodePort` (`service.type`), a load balancer or an ingress.

With the Prometheus Operator, `serviceMonitor.enabled=true` adds a
ServiceMonitor that scrapes `/metrics` with the agent key as its Bearer
token, read from the identity Secret. The chart refuses to render one on
a cluster without the ServiceMonitor CRD; with `helm template`, pass
`--api-versions monitoring.coreos.com/v1/ServiceMonitor`. Set
`serviceMonitor.labels` to what your Prometheus selects on, often
`release: <prometheus release>`.

## State

`/var/lib/senhub-agent` holds the bookmarks of the log probes. By
default it is an `emptyDir`: when the pod is replaced, a file probe
starts again at the end of each file and a Container Apps stream
re-sends its recent lines. `persistence.enabled=true` puts it on a
volume. The identity does not depend on it.

## Security

The pod runs as uid 10001, non-root, with a read-only root filesystem,
no privilege escalation, every capability dropped and the runtime's
default seccomp profile. The directories the entrypoint and the agent
write to (configuration, state, logs, `/tmp`) are volumes.

## Why one replica

`replicas` is not a value. Each replica would carry the same identity
and run the same probes against the same targets: every measurement,
log line and Kubernetes event would be sent once per replica, under one
agent key. For more targets, or another cluster, install the chart
again under another release name; each release has its own identity.
The Deployment uses the `Recreate` strategy for the same reason: the old
pod stops before the new one starts.

## What the chart does not do

- **No DaemonSet, no node metrics.** The agent's host probes (CPU,
  memory, disks, network) measure the machine they run on. In this pod
  they only have the container's view of it: its own filesystems and
  network interface, and kernel counters that are partly the node's,
  reported under the pod's identity. That is neither a faithful node nor
  a faithful container, so read them as the agent's own surroundings,
  not as node monitoring. The image has no access to the node's
  filesystems, processes or devices, and the chart grants none. A
  DaemonSet that watches each node would need host mounts, the host's
  process and network namespaces and a per-node identity, a different
  security profile that this version does not ship. The
  `kubernetes` probe reports node readiness, capacity and pressure from
  the API server; for per-node resource use, run the agent on the nodes
  as a service, or use the metrics your distribution already exposes.
- **No registry.** The chart is installed from the repository for now.
- **No auto-update.** A new version is a new image tag: `image.tag`, or
  the chart's `appVersion`.
- **No console-made configuration survives a new pod.** See
  [Configuring](#configuring).
