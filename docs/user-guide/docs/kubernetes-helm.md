# Kubernetes (Helm)

A Helm chart runs the agent in a Kubernetes cluster: by default one agent
on every node (a DaemonSet), the [container image](container.md), an
identity of its own on each node, and, when you ask for it, read-only
monitoring of the cluster itself through the
[`kubernetes` probe](probes/kubernetes.md).

The chart is published with each release as an OCI artifact on
`ghcr.io`, with the release version as its version, so it installs
without a clone (Helm 3.8 or later). Its source is in the agent's
repository under `charts/senhub-agent`.

## Install

```bash
kubectl create namespace senhub

# The agent monitors the node it runs on (host namespaces, see below):
# the namespace must allow the privileged Pod Security level.
kubectl label namespace senhub pod-security.kubernetes.io/enforce=privileged

# Credentials live in a Secret; its keys become environment variables.
kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN='<your token>'

helm install senhub-agent oci://ghcr.io/senhub-io/charts/senhub-agent \
  --version <version> -n senhub \
  --set secrets.existingSecret=senhub-agent-credentials
```

`<version>` is a release, for example `0.6.2`; a beta (`0.6.2-beta.1`) is
installed the same way, with its own version. The chart installs the
image of the same release unless `image.tag` says otherwise. To install
from a clone instead, replace the `oci://` address with
`./senhub-agent/charts/senhub-agent`.

That is a first run: the agent watches the node it runs on and pushes to
SenHub. It needs no licence: the free tier collects everything except the
Pro probes.

To use a Pro probe, add the licence to the same Secret and restart the
pod:

```bash
kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN='<your token>' \
  --from-literal=SENHUB_LICENSE='<licence JWT>' \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n senhub rollout restart deployment/senhub-agent
```

`--set edition=oss` selects the free build of the image, which does not
contain the Pro probes at all.

Check it started, and read the lines the entrypoint writes about the
configuration it built:

```bash
kubectl -n senhub logs deploy/senhub-agent -c agent
kubectl -n senhub port-forward svc/senhub-agent 8080:8080
curl http://127.0.0.1:8080/health
```

## Monitoring the node

`hostMonitoring.enabled` is `true` by default, and the chart then
installs a **DaemonSet**: one agent on each node, monitoring that node.
(`kind: Deployment` keeps a single agent for the release, for container
scope, a single node or remote targets only.)

The pod runs in the node's PID and network namespaces
(`hostPID`, `hostNetwork`) and mounts the node's `/` read-only at
`/host`, with propagation so the node's other mounts appear under it.
Disks, network interfaces and processes are then the node's: the disk
probe lists the node's mounts (without the `/host` prefix, and without
the bind mounts of the container runtime and the kubelet), the network
probe the node's interfaces, the process probe every process of the
node, and the OS release, boot time and host identity (`machine-id`) are
the node's.

Without it (`hostMonitoring.enabled=false`), the pod's CPU and memory
figures are still the node's, because `/proc` is not isolated for those
counters, but the disks are the container's overlay, the network is the
pod's single interface and the processes are the pod's own. That is half
a node, which is why the default is the node.

!!! warning "What you accept"
    - The pod shares the node's PID and network namespaces and can read
      the whole node filesystem. It stays read-only, uid 10001, with no
      capability and no privilege escalation, so it reads what any user
      can, not root-only files. Kubernetes' `baseline` level forbids
      host namespaces and hostPath: label the namespace
      `pod-security.kubernetes.io/enforce=privileged`.
    - The pods share the node's network, so the HTTP output would be
      reachable on every address of the node, without TLS. In the
      DaemonSet it therefore listens on the node's loopback
      (`127.0.0.1`) until you open it, and the kubelet probes follow it.
      See [Opening the HTTP output](#opening-the-http-output).
    - The DaemonSet runs on the nodes it tolerates: not the control-plane
      nodes by default, since they carry a `NoSchedule` taint. Add the
      toleration to monitor them:

        ```yaml
        tolerations:
          - key: node-role.kubernetes.io/control-plane
            operator: Exists
            effect: NoSchedule
        ```

      `nodeSelector` narrows it to some nodes. Updates roll one node at
      a time (`maxUnavailable: 1`).
    - Each agent keeps its identity on its node, see [Identity](#identity).
      The host identity is the node's `machine-id`, and the agent key is
      generated at the node's first start.

The cluster probe runs once for the whole cluster, not once per node, so
it is not available in the DaemonSet (the chart refuses to render the
two together). Install a second release for it:
`--set hostMonitoring.enabled=false --set kubernetesProbe.enabled=true --set rbac.kubernetesProbe.enabled=true`
(a Deployment). The same goes for a `ServiceMonitor` and for a whole
`config.agent`, which carry one key for every agent.

## Monitoring the cluster

Two values turn it on: one adds the probe, the other the read-only
permissions it needs. It lives in its own release, a Deployment, next to
the node agents:

```bash
helm install senhub-cluster oci://ghcr.io/senhub-io/charts/senhub-agent \
  --version <version> -n senhub \
  --set secrets.existingSecret=senhub-agent-credentials \
  --set hostMonitoring.enabled=false \
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

**DaemonSet.** Each node has its own identity, kept in the state
directory of that node, `/var/lib/senhub-agent` (`daemonSet.stateHostPath`),
a hostPath created if missing and handed to uid 10001 by an init
container with `CAP_CHOWN` (`fsGroup` does not apply to a hostPath).
The agent key is generated at the node's first start and restored at
every later one, so it survives pod restarts and upgrades. The host
identity is the node's own `machine-id`. There is no shared Secret and
no volume claim: one identity for all nodes would make them one agent.
Read a node's key, for PRTG or Nagios, from its pod:

```bash
kubectl -n senhub exec <pod> -c agent -- cat /var/lib/senhub-agent/agent.key
```

If another agent runs on the node as a service, it uses the same default
directory: change `daemonSet.stateHostPath`.

**Deployment.** One identity for the release, as follows. It is kept in
the Secret `<release>-identity`, which `helm uninstall` leaves in place
(`helm.sh/resource-policy: keep`); the state claim `<release>-state`,
when `persistence` is on, is kept the same way. Delete both to start
over (see [State](#state)).

The agent key (what PRTG, Nagios and a Prometheus scrape read with, and
what tells two agents apart downstream) and the host identity are
generated at first install and kept in the Secret
`<release>-identity`. The pod receives them as `SENHUB_AGENT_KEY` and
`SENHUB_HOST_ID`, and the host identity is also mounted as
`/etc/machine-id`, where the agent reads it (with host monitoring, the
agent reads the node's own `machine-id` instead). A pod replaced, rescheduled
on another node or upgraded to a new image is the same host and the same
agent.

A Secret rather than a volume, because it follows the pod to any node
and any zone and needs no storage class. The Secret is kept when the
release is uninstalled, so reinstalling under the same name in the same
namespace brings the same agent back; delete it to start over:

```bash
kubectl -n senhub delete secret senhub-agent-identity
```

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

The host *name* is the node's with host monitoring; otherwise the pod's
host name, set to the release name so it does not change at every
rollout (`hostname` sets another).

## Opening the HTTP output

PRTG, Nagios and a Prometheus scrape reach a node's agent on
`<node address>:8080`, which needs the output to listen beyond the
loopback. Open it deliberately, and turn TLS on, since the agent key
travels in every request:

```yaml
config:
  strategies:
    http:
      port: 8080
      bind_address: 0.0.0.0     # or the node address your monitoring reaches
      endpoints: ["prtg", "nagios", "prometheus"]
      tls:
        enabled: true           # self-signed pair generated at first start,
                                # see HTTP / HTTPS to bring your own
```

Restrict who may connect with the node's firewall or a network policy of
your CNI (host-network pods are often outside `NetworkPolicy`, so the
firewall is the reliable place). `http.bind: 0.0.0.0` alone opens it
without TLS: do that only on a trusted network. A Deployment, whose pod
network is the isolation, listens on every address by default.

## Reading the agent

In the DaemonSet there is no Service: each agent answers on its node,
`http://<node address>:8080` (the pods use the host network), with that
node's key. Point PRTG or Nagios at the node.

With a Deployment, the Service `<release>` exposes the HTTP output on port 8080: PRTG,
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

With a DaemonSet the state is on each node (see [Identity](#identity)),
and `persistence` does not apply. It stays on the nodes after
`helm uninstall`, so a reinstall brings each node's agent back. To start
over, delete the directory on every node:

```bash
sudo rm -rf /var/lib/senhub-agent
```

With a Deployment, `/var/lib/senhub-agent` holds the bookmarks of the log probes. By
default it is an `emptyDir`: when the pod is replaced, a file probe
starts again at the end of each file and a Container Apps stream
re-sends its recent lines. `persistence.enabled=true` puts it on a
volume. The identity does not depend on it.

The claim `<release>-state` is kept when the release is uninstalled
(`helm.sh/resource-policy: keep`), like the identity Secret, so that a
reinstall finds its bookmarks. To remove it:

```bash
helm uninstall senhub-agent -n senhub
kubectl -n senhub delete pvc senhub-agent-state
```

## Log level

`logLevel: debug` starts the agent with `--verbose`; `logFilter: probe`
(a module name prefix) limits the debug lines to those modules. Read
them with `kubectl logs`.

## Security

The pod runs as uid 10001, non-root, with a read-only root filesystem,
no privilege escalation, every capability dropped and the runtime's
default seccomp profile. Host monitoring adds host namespaces and a
read-only mount of the node's root, described [above](#monitoring-the-node). The directories the entrypoint and the agent
write to (configuration, state, logs, `/tmp`) are volumes.

## Why one replica (Deployment)

`replicas` is not a value. Each replica would carry the same identity
and run the same probes against the same targets: every measurement,
log line and Kubernetes event would be sent once per replica, under one
agent key. For more targets, or another cluster, install the chart
again under another release name; each release has its own identity.
The Deployment uses the `Recreate` strategy for the same reason: the old
pod stops before the new one starts.

## What the chart does not do

- **No cluster probe in the DaemonSet.** It would collect the whole
  cluster once per node. Use a second, Deployment release.
- **No registry.** The chart is installed from the repository for now.
- **No auto-update.** A new version is a new image tag: `image.tag`, or
  the chart's `appVersion`.
- **No console-made configuration survives a new pod.** See
  [Configuring](#configuring).
