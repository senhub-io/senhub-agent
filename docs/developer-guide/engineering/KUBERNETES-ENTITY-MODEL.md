# Kubernetes entity model — what a cluster contributes to the graph

**Status:** analysis, 2026-08-11. Input for the Toise conversation; nothing
here is implemented beyond the cluster entity.
**Audience:** agent maintainers, and the Toise team as the vocabulary owner.

Today the probe emits **one** entity — the cluster — and everything else is
metrics only. So a pod, a node and a workload exist as series and as nothing
else: they cannot be pivoted to, related, or found in the graph.

This asks a question the frozen vocabulary does not answer on its face: a
cluster manages objects that have no obvious counterpart in twelve types
agreed for physical and service infrastructure.

## The rule that decides most of it

Two constraints from the contract do most of the work here.

**Identity must be a property of the thing** (C2). Kubernetes is unusually
good on this point: every object carries a `UID` the API server assigns at
creation and never reissues, and the runtime reports container ids. There is
no case here where we would have to fall back on an address, which is the
failure mode that produced #740.

**A type must exist in the frozen vocabulary.** An unregistered type is
dropped at the consumer's boundary. So the question is not "what would we
like to model" but "which of the twelve types does each object actually
*is*".

## Per object

### Node → `host`, and it reconciles with an agent inside it

A Kubernetes node is a machine with its own operating system. That is a
`host` by the boundary rule, not a new concept.

What makes this worth doing rather than merely correct: the API reports
`Status.NodeInfo.MachineID`, which comes from `/etc/machine-id` — **the same
file gopsutil reads for our own `host.id`**. A node entity keyed on it is
therefore byte-identical to the entity an agent running *inside* that node
emits for itself.

The two observations converge on one node in the graph instead of producing a
duplicate. The cluster view contributes the scheduling facts, the in-guest
agent contributes CPU, memory and disk, and they describe the same machine
because they agree on its identity.

**The caveat turned out to be the defect, and not for the reason stated.**

The paragraph that stood here worried about a node *without* `/etc/machine-id`.
The real problem was on the nodes that have one. Measured (#762):

```
/etc/machine-id                     8b86170405bc4382b0577eac3df5e730
agent host.id (emitted)             8b861704-05bc-4382-b057-7eac3df5e730
```

Kubernetes returns the file verbatim; gopsutil formats the same bytes as a
dashed UUID. Same machine, same file, **two spellings** — so two entities, a
silent duplicate for every node of every cluster, which is the precise
opposite of what this model promises.

The reasoning above was sound and the conclusion was wrong. Verifying that
both sides read the same *file* is not verifying that both emit the same
*string*. That is C6's equality requirement, and it was skipped because the
derivation looked too obviously identical to check.

The emitted value is now normalised to the agent's spelling. `SystemUUID`
remains a descriptive attribute and the natural `same_as` facet — never the
identity, being absent or forged on many virtualisation platforms.

**What is still unproven:** that the two entities actually merge in the graph.
That needs a node which is both a Kubernetes node and a machine running an
agent. k3d cannot show it — its nodes are containers reporting an empty
MachineID, so the lab run produced no node entities at all.

### Container → `container`, and it reconciles with the docker probe

`ContainerStatus.ContainerID` is runtime-reported and stable for the life of
the container (`containerd://<sha>`). The vocabulary has `container`, keyed
on `container.id`, and the docker probe already emits exactly that.

So a container observed from the cluster and the same container observed by a
docker probe on the node converge — provided the id is normalised. The k8s
form carries a runtime scheme prefix the docker probe does not. **The prefix
must be stripped, or both sides must carry it.** Getting this wrong produces
two entities per container, which is worse than emitting neither: it doubles
the graph and makes every count wrong.

That normalisation is a decision to take with Toise, not unilaterally,
because the docker probe's form is already in their graph.

### Pod → no type fits, and inventing one is a contract change

A pod is the scheduling unit: one or more containers sharing a network
namespace and a lifetime. It is not a container, not a host, not a service
instance.

Three options, in order of preference:

1. **Do not model it.** The pod name rides as an attribute on the container
   entities and as a metric label. Nothing is lost that a query cannot
   reconstruct, and the graph stays smaller by roughly the count of pods.
2. **Ask Toise for a `k8s.pod` type.** Honest, and the contract has a process
   for it — but it is a new type in a vocabulary deliberately kept to twelve,
   and it buys a node whose only relations are "contains these containers"
   and "runs on this node", both of which the container entities already
   express.
3. Map it to `container`. **Wrong** — a pod with three containers would
   collide with them or triple-count.

**Recommendation: option 1**, and raise it with Toise as a decision rather
than a silence. The cost of a missing node is a query the consumer must
write; the cost of a wrong node is a graph nobody trusts.

### Workloads (Deployment, StatefulSet, DaemonSet, Job, CronJob) → `service.instance`

A workload declares an application and keeps it running. By the boundary rule
it carries no `db.system`, so it is a `service.instance`.

Identity: the object `UID`, which Kubernetes assigns and persists — not
`namespace/name`, which is editable and reused. A Deployment deleted and
recreated with the same name is a *different* workload, and the UID says so
where the name would lie.

This is the one place where the cluster contributes a node the rest of the
infrastructure cannot see at all: nothing outside the cluster knows a
StatefulSet exists.

### Namespace, Service, Ingress, PersistentVolume → not now

Namespaces are a grouping, better as an attribute than a node. Services and
Ingresses are routing, and the vocabulary's `network.*` types describe
physical topology rather than virtual routing — forcing them in would say
something false. PersistentVolumes are the most defensible future candidate,
and can wait for a use case.

## Relations

Only edges that are true regardless of the observer, per the contract:

| Edge | Meaning |
|---|---|
| `container --runs_on--> host` | the container runs on that node; the node is a `host` |
| `service.instance(workload) --runs_on--> service.instance(cluster)` | the workload is scheduled by that cluster |
| `service.instance(agent) --monitors--> service.instance(cluster)` | already emitted |

Deliberately **not** emitted: an edge from workload to each of its containers.
Kubernetes recreates containers constantly, so that edge would churn at the
rate of pod restarts, and it is derivable from the container's pod attribute
plus the workload's selector on the consumer side.

## What this asks of Toise

1. **A decision on the pod**, per the options above. Our recommendation is
   not to model it.
2. **A normalisation for `container.id`** between the k8s runtime form
   (`containerd://<sha>`) and the docker probe's form, since both land in
   their graph and must not double.
3. **Confirmation that node reconciliation is wanted**, i.e. that a k8s node
   and an in-guest agent describing the same machine should be one entity.
   We believe it is the whole point, but it changes what their node counts
   mean and they should say so rather than discover it.

Nothing here needs a new relation type, and only option 2 for the pod would
need a new entity type.

## Ordering

Identity before labels, as always: none of these entities ship before the
`container.id` normalisation is agreed, because emitting a colliding or
doubled identity turns a gap a consumer can see into a wrong answer it
cannot.

## References

- `ENTITY-TELEMETRY-CONTRACT.md` — C2 (identity is a property of the thing),
  C4 (declare where the telemetry is), the ordering constraint
- `.claude/rules/probes.md` — the `db` vs `service.instance` boundary rule,
  the entity-type table
- #756 — Kubernetes coverage
- `k8sclusterreceiver` — the metric naming reference
