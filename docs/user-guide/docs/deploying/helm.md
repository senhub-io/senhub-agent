<!-- Publish with feat/helm-chart: this page describes the chart that branch adds (charts/senhub-agent) and links to its full page, kubernetes-helm.md. -->

# Helm

The chart in `charts/senhub-agent` runs the agent on Kubernetes: by default
one agent on every node (a DaemonSet), each with an identity of its own.
This page is the deployment runbook; the full description of what the agent
watches, the identity rules and every value is
[Kubernetes (Helm)](../kubernetes-helm.md).

The chart is installed from the repository for now, not from a registry.

## A values file

Keep everything but the secrets in a file under version control.
`values-paris.yaml`:

```yaml
edition: oss
image:
  tag: "0.6.2"

env:
  otlpEndpoint: collector.observability.svc:4317
  tags:
    environment: production
    cluster: paris-1

config:
  probes:
    databases:
      - name: orders-db
        type: postgresql
        params:
          host: orders-db.shop.svc
          username: monitor
          password: "${env:ORDERS_DB_PASSWORD}"
```

The password is a reference: the value is read from the environment of the
pod, and the environment comes from a Secret.

## Install

```bash
git clone --branch <tag of a release that carries the chart> \
  https://github.com/senhub-io/senhub-agent.git

kubectl create namespace senhub
kubectl label namespace senhub pod-security.kubernetes.io/enforce=privileged

kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN="$OTLP_TOKEN" \
  --from-literal=ORDERS_DB_PASSWORD="$PG_PASSWORD"

helm install senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  -f values-paris.yaml \
  --set secrets.existingSecret=senhub-agent-credentials
```

!!! note "Not yet verified on a cluster"
    The recette ran the install with `--set` values and the chart's own
    values files. This exact form, a values file combined with
    `secrets.existingSecret`, is expected to behave the same and has not
    been run as written.

The `privileged` label is needed because the default is to watch the node
(host namespaces and a read-only mount of its filesystem). To watch only the
pod, set `hostMonitoring.enabled=false`: the workload becomes a Deployment
and the namespace can stay at the default level. What the node mode accepts
is listed under
[Monitoring the node](../kubernetes-helm.md#monitoring-the-node).

## Pinning

Two things move, and both are pinned:

- **The image**: `image.tag` is an exact version (`0.6.2`). Left empty it
  follows the chart's `appVersion`, which is the version of the checkout. There
  is no `latest`.
- **The chart**: the clone is the chart. Check out a release tag, not
  `master`, so a `git pull` cannot change the manifests under you.

`edition: oss` selects the open source image; `full` (the default) the
image that holds the paid probes.

## Licence

The free tier needs none. For the paid probes (with `edition: full`), the
token is a key of the same Secret, as an environment variable:

```bash
kubectl -n senhub create secret generic senhub-agent-credentials \
  --from-literal=OTLP_BEARER_TOKEN="$OTLP_TOKEN" \
  --from-literal=ORDERS_DB_PASSWORD="$PG_PASSWORD" \
  --from-literal=SENHUB_LICENSE="$(cat license.jwt)" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n senhub rollout restart daemonset/senhub-agent
```

(`deployment/senhub-agent` with a Deployment.) This licence step has not
been verified on a cluster yet. The licence is read at the pod's start,
hence the restart. A licence is the same token for every agent
of a customer, so one Secret serves every cluster you own.

## Secrets

A credential never goes in the values: they end in a ConfigMap. It goes in a
Secret whose keys become environment variables, and the configuration refers
to it with `${env:NAME}`, as above. Either name a Secret you manage
(`secrets.existingSecret`, the form that fits an external secret operator
or sealed secrets) or let the chart create one from a file kept out of
version control (`secrets.values`).

Secrets mounted as files work as well, with the agent's own rule: an
`extraVolumes` entry for the Secret, an `extraVolumeMounts` entry under
`/run/secrets`, and `SENHUB_PROBE_<NAME>_<PARAM>_FILE` in `extraEnv`. See
[Probes from environment variables](../configuration.md#configuring-probes-from-environment-variables).

Changing an existing Secret does not roll the pods. Restart them.

## Validation

From the cheapest, before and after the apply.

```bash
helm lint ./senhub-agent/charts/senhub-agent -f values-paris.yaml
helm template senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  -f values-paris.yaml --set secrets.existingSecret=senhub-agent-credentials \
  | kubectl apply --dry-run=server -f -
```

The server-side dry run of `helm upgrade --install --dry-run=server` has not
been verified yet and is left out.

The chart ships a `values.schema.json`, so a value of the wrong type or an
unknown edition fails at `helm lint` with its name. It also refuses
combinations that do not make sense, such as the Kubernetes probe in a
DaemonSet.

The configuration itself is checked twice by the image's entrypoint, which
refuses an error and stops the pod with the reason, and by hand once the
pod runs:

```bash
kubectl -n senhub logs ds/senhub-agent -c agent | grep "configuration written and checked"
kubectl -n senhub exec ds/senhub-agent -c agent -- senhub-agent config check --json
```

Exit `0` is clean, `1` warnings only, `2` an error.

## Upgrade

Change `image.tag` (and check out the matching chart tag), then:

```bash
helm upgrade senhub-agent ./senhub-agent/charts/senhub-agent -n senhub \
  -f values-paris.yaml --set secrets.existingSecret=senhub-agent-credentials
kubectl -n senhub rollout status ds/senhub-agent
```

A DaemonSet rolls one node at a time. Each node keeps its identity in its
state directory, so an agent comes back as the same agent. The agent's own
auto-update is off in a container.

`helm rollback` has not been verified on a cluster yet; to go back, set the
previous `image.tag` and upgrade again.

## Removal

```bash
helm uninstall senhub-agent -n senhub
```

This leaves, on purpose, what a reinstall needs: the per-node state
directory of a DaemonSet (`/var/lib/senhub-agent` on each node), and for a
Deployment the identity Secret and the `<release>-state` claim. Remove them
for a clean slate:

```bash
kubectl -n senhub delete secret senhub-agent-credentials   # your own Secret
# Deployment only:
kubectl -n senhub delete secret senhub-agent-identity
kubectl -n senhub delete pvc senhub-agent-state
# DaemonSet: on every node
sudo rm -rf /var/lib/senhub-agent
```

## Verify

```bash
kubectl -n senhub get ds,pods -o wide
kubectl -n senhub rollout status ds/senhub-agent
kubectl -n senhub logs ds/senhub-agent -c agent | grep -E "configuration|identity"
kubectl -n senhub exec ds/senhub-agent -c agent -- senhub-agent --version
kubectl -n senhub exec ds/senhub-agent -c agent -- senhub-agent config check; echo "exit $?"
kubectl -n senhub exec ds/senhub-agent -c agent -- senhub-agent license show
kubectl -n senhub get ds senhub-agent -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
kubectl -n senhub get ds senhub-agent -o yaml | grep -c "$PG_PASSWORD" || true
```

Expected: one pod per schedulable node, `Running` and ready, the pinned
image tag, `config check` exit `0`, the licence tier you provisioned, and
the last count at `0`: no secret value appears in the workload's manifest.

Reaching a DaemonSet agent from PRTG or Nagios takes the HTTP output opened
on the node, deliberately and with TLS; see
[Opening the HTTP output](../kubernetes-helm.md#opening-the-http-output).
