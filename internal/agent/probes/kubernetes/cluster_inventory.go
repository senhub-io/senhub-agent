package kubernetes

import (
	"strings"

	corev1 "k8s.io/api/core/v1"

	"senhub-agent.go/internal/agent/services/entity"
)

// The objects a cluster manages, as graph entities.
//
// Agreed with the Toise team (2026-08-11); the reasoning lives in
// KUBERNETES-ENTITY-MODEL.md. Two of the three decisions are implemented here;
// the pod is blocked on the consumer publishing the type, and the container's
// runs_on target moves from the node to the pod when it does — an edge change,
// not a re-key.

// nodeEntity returns the host half of a Kubernetes node's graph presence.
//
// The inventory splits a node into two entities when the node reports both a
// canonical MachineID and a UID: the host carries the machine facts, and the
// k8s.node carries its role in the cluster. The two are joined by a same_as
// relation from the k8s.node to the host; the probe reads both identities from
// the same API object, which is the evidence for that relation.
//
// This function emits the host side only, and only when the node's MachineID
// is canonical. The MachineID is what the kubelet reads from /etc/machine-id,
// the file the agent's own host.id comes from, so the host emitted here and
// the one an agent inside the node emits converge on one graph node (ADR 0018). A missing MachineID is a visible hole that an in-guest agent
// fills; a fallback would mint a silent duplicate.
//
// SystemUUID is carried as a descriptive attribute rather than used as the
// fallback: it is absent or forged on several virtualisation platforms, and it
// is the natural same_as facet the day the consumer implements that overlay.
func nodeEntity(n *corev1.Node, _ string) (entity.Entity, bool) {
	machineID := canonicalMachineID(n.Status.NodeInfo.MachineID)
	if machineID == "" {
		return entity.Entity{}, false
	}

	attrs := map[string]any{
		"host.name":         n.Name,
		"os.type":           n.Status.NodeInfo.OperatingSystem,
		"host.arch":         n.Status.NodeInfo.Architecture,
		"k8s.node.os_image": n.Status.NodeInfo.OSImage,
	}
	if uuid := strings.TrimSpace(n.Status.NodeInfo.SystemUUID); uuid != "" {
		attrs["host.uuid"] = uuid
	}

	return entity.Entity{
		Type:       entity.TypeHost,
		ID:         map[string]any{"host.id": machineID},
		Attributes: attrs,
	}, true
}

// nodeK8sEntity returns the k8s.node half of a Kubernetes node's graph
// presence. It is emitted whenever the node has a UID, even when the machine
// id is absent.
func nodeK8sEntity(n *corev1.Node, clusterUID string) (entity.Entity, bool) {
	uid := strings.TrimSpace(string(n.UID))
	if uid == "" {
		return entity.Entity{}, false
	}

	attrs := map[string]any{
		"k8s.node.name":          n.Name,
		"k8s.kubelet.version":    n.Status.NodeInfo.KubeletVersion,
		"k8s.container.runtime":  n.Status.NodeInfo.ContainerRuntimeVersion,
		"k8s.node.unschedulable": n.Spec.Unschedulable,
	}
	if clusterUID != "" {
		attrs["k8s.cluster.uid"] = clusterUID
	}
	for _, cond := range n.Status.Conditions {
		key, ok := nodeConditionAttributeName(cond.Type)
		if !ok {
			continue
		}
		attrs[key] = strings.ToLower(string(cond.Status))
	}

	return entity.Entity{
		Type:       entity.TypeK8sNode,
		ID:         map[string]any{"k8s.node.uid": uid},
		Attributes: attrs,
	}, true
}

// nodeConditionAttributeName maps the four Kubernetes node conditions we keep
// to their graph attribute spellings, reusing the existing snake_case
// convention for multiword names.
func nodeConditionAttributeName(t corev1.NodeConditionType) (string, bool) {
	switch t {
	case corev1.NodeReady:
		return "k8s.node.condition.ready", true
	case corev1.NodeMemoryPressure:
		return "k8s.node.condition.memory_pressure", true
	case corev1.NodeDiskPressure:
		return "k8s.node.condition.disk_pressure", true
	case corev1.NodePIDPressure:
		return "k8s.node.condition.pid_pressure", true
	default:
		return "", false
	}
}

// sameAsK8sNodeHost returns the same_as edge tying the cluster's node entity to
// the host machine entity. The API object is the evidence for both identities.
func sameAsK8sNodeHost(nodeEnt, hostEnt entity.Entity) (entity.Relation, bool) {
	if emptyID(nodeEnt.ID) || emptyID(hostEnt.ID) {
		return entity.Relation{}, false
	}
	return entity.Relation{
		Type:     entity.RelSameAs,
		FromType: nodeEnt.Type,
		FromID:   nodeEnt.ID,
		ToType:   hostEnt.Type,
		ToID:     hostEnt.ID,
		Attributes: map[string]any{
			"basis":      "k8s_api",
			"confidence": 1.0,
		},
	}, true
}

// nodeInventory returns all entities and relations a single Kubernetes node
// contributes to the graph: the host machine half, the k8s.node role half, and
// — when both halves exist — the same_as edge joining them.
func nodeInventory(n *corev1.Node, clusterUID string) ([]entity.Entity, []entity.Relation) {
	var entities []entity.Entity
	var relations []entity.Relation

	hostEnt, hasHost := nodeEntity(n, clusterUID)
	if hasHost {
		entities = append(entities, hostEnt)
	}

	nodeEnt, hasK8sNode := nodeK8sEntity(n, clusterUID)
	if hasK8sNode {
		entities = append(entities, nodeEnt)
	}

	if hasHost && hasK8sNode {
		if rel, ok := sameAsK8sNodeHost(nodeEnt, hostEnt); ok {
			relations = append(relations, rel)
		}
	}

	return entities, relations
}

// canonicalMachineID renders a machine-id the way the agent's own host entity
// does, so the two spellings of one machine are one string.
//
// This is the trap that nearly shipped. Kubernetes returns /etc/machine-id
// verbatim — 32 hex characters, no separators. gopsutil, which the agent uses
// for its own host.id, formats the same bytes as a dashed UUID. Measured on a
// real machine:
//
//	/etc/machine-id      8b86170405bc4382b0577eac3df5e730
//	agent host.id        8b861704-05bc-4382-b057-7eac3df5e730
//
// Same machine, same file, two spellings — and therefore two entities, a
// silent duplicate for every node in every cluster. Verifying that both sides
// read the same FILE was not enough; only comparing the emitted STRINGS caught
// it. That is C6's equality requirement, applied to a value nobody thought to
// compare because the derivation looked obviously identical.
//
// Anything that is not a bare 32-character hex id is returned trimmed and
// unchanged: a value already dashed is left alone, and an unexpected shape is
// not reformatted into a plausible-looking identity that would be wrong.
func canonicalMachineID(raw string) string {
	id := strings.TrimSpace(raw)
	if len(id) != 32 {
		return id
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return id
		}
	}
	id = strings.ToLower(id)
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32]
}

// containerEntity turns a container status into a container entity.
//
// Identity is the full lowercase sha with the runtime scheme stripped. The
// Kubernetes form is `containerd://<sha>` (or `docker://`, `cri-o://`): the
// scheme names the path by which the container was observed, not the
// container. A container does not change identity depending on whether it is
// seen through containerd or through Docker — C2, applied to a type the docker
// probe already emits in the canonical form.
//
// Getting this wrong does not lose data, it doubles it: the same container
// observed from the cluster and by a docker probe on the node would become two
// entities, and every count built on them would be wrong. That is why nothing
// here shipped before the form was agreed.
func containerEntity(cs *corev1.ContainerStatus, pod *corev1.Pod, nodeMachineID string) (entity.Entity, bool) {
	id := normalizeContainerID(cs.ContainerID)
	if id == "" {
		// A container that has never started has no runtime id. It is not a
		// container yet — emitting one keyed on nothing would be inventing it.
		return entity.Entity{}, false
	}

	attrs := map[string]any{
		"container.name":     cs.Name,
		"container.image":    cs.Image,
		"k8s.pod.name":       pod.Name,
		"k8s.namespace.name": pod.Namespace,
	}
	if runtime := containerRuntime(cs.ContainerID); runtime != "" {
		attrs["container.runtime"] = runtime
	}
	if nodeMachineID != "" {
		attrs["k8s.node.name"] = pod.Spec.NodeName
	}

	return entity.Entity{
		Type:       entity.TypeContainer,
		ID:         map[string]any{"container.id": id},
		Attributes: attrs,
	}, true
}

// normalizeContainerID strips the runtime scheme and lowercases the sha.
//
// Returns "" for anything that is not a usable id, rather than guessing: an
// empty or malformed value must produce no entity at all, since an entity with
// a fabricated identity is worse than a missing one.
func normalizeContainerID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	if idx := strings.Index(id, "://"); idx >= 0 {
		id = id[idx+3:]
	}
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ""
	}
	return id
}

// containerRuntime recovers the scheme as a descriptive attribute — the fact
// is worth keeping, it simply is not part of the identity.
func containerRuntime(raw string) string {
	if idx := strings.Index(raw, "://"); idx > 0 {
		return strings.ToLower(raw[:idx])
	}
	return ""
}

// podEntity turns a pod into a pod entity.
//
// Identity is the Kubernetes UID, never namespace/name: the pair is editable
// and reused, so a pod deleted and recreated under the same name would inherit
// the history of a different one. Same reasoning as the workloads, and as the
// databases before them.
//
// The type exists because the pod owns telemetry no container has — the
// network namespace is shared, so network measurements belong to the pod and
// to nothing else. That is what settled the question: a type with own-key
// telemetry needs an entity to hang it on, which is C4 deciding rather than a
// preference about what reads well.
func podEntity(pod *corev1.Pod) (entity.Entity, bool) {
	uid := strings.TrimSpace(string(pod.UID))
	if uid == "" {
		return entity.Entity{}, false
	}
	attrs := map[string]any{
		"k8s.pod.name":       pod.Name,
		"k8s.namespace.name": pod.Namespace,
		"k8s.node.name":      pod.Spec.NodeName,
	}
	if pod.Spec.ServiceAccountName != "" {
		attrs["k8s.serviceaccount.name"] = pod.Spec.ServiceAccountName
	}
	// The workload this pod belongs to — the first thing anyone wants to know
	// looking at a pod, and until now absent from the graph entirely. A pod
	// named api-7d75d55c8f-vmj8z says nothing on its own; that it belongs to
	// the api Deployment says everything.
	//
	// Read from the owner reference rather than parsed out of the name: the
	// name's shape is a convention, not a contract, and a pod created directly
	// has no owner at all. The workload is an attribute and not an entity for
	// the same reason a Swarm service is not one — no relation in the
	// vocabulary means "belongs to", and stretching runs_on to mean it is the
	// overload refused for network segments.
	if kind, name := podOwner(pod); name != "" {
		attrs["k8s.workload.kind"] = kind
		attrs["k8s.workload.name"] = name
	}
	return entity.Entity{
		Type:       entity.TypePod,
		ID:         map[string]any{"k8s.pod.uid": uid},
		Attributes: attrs,
	}, true
}

// runsOn builds one placement edge.
//
// The chain is container → runs_on → pod → runs_on → host, which is the shape
// agreed with the consumer. runs_on propagates failure from target to source,
// so a node going down takes its pods, which take their containers: an impact
// query answers transitively with no extra code. The consumer measured exactly
// that on their test instance before publishing the type.
//
// An edge whose target identity is empty is skipped rather than emitted: a
// relation whose target never materialises is buffered and then dropped by the
// consumer, costing a warning and buying nothing.
func runsOn(fromType string, fromID map[string]any, toType string, toID map[string]any) (entity.Relation, bool) {
	if emptyID(fromID) || emptyID(toID) {
		return entity.Relation{}, false
	}
	return entity.Relation{
		Type:     entity.RelRunsOn,
		FromType: fromType,
		FromID:   fromID,
		ToType:   toType,
		ToID:     toID,
	}, true
}

func emptyID(id map[string]any) bool {
	if len(id) == 0 {
		return true
	}
	for _, v := range id {
		if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
			return true
		}
	}
	return false
}

// podOwner returns the kind and name of the workload that created this pod.
//
// A ReplicaSet is reported as the Deployment that owns it when the name allows
// it: Kubernetes inserts a ReplicaSet between a Deployment and its pods, and
// nobody thinks in ReplicaSets — an operator asks about the Deployment. The
// generated suffix is stripped only when it looks like one, so a ReplicaSet
// created directly keeps its own name rather than being renamed into a
// Deployment that does not exist.
func podOwner(pod *corev1.Pod) (kind, name string) {
	for _, ref := range pod.OwnerReferences {
		if ref.Controller == nil || !*ref.Controller {
			continue
		}
		if ref.Kind == "ReplicaSet" {
			if base, ok := deploymentNameOf(ref.Name); ok {
				return "Deployment", base
			}
		}
		return ref.Kind, ref.Name
	}
	return "", ""
}

// deploymentNameOf strips the ReplicaSet's pod-template hash suffix. Returns
// ok=false when the trailing segment does not look like a generated hash, so a
// hand-made ReplicaSet is never reported as a Deployment.
func deploymentNameOf(rsName string) (string, bool) {
	i := strings.LastIndex(rsName, "-")
	if i <= 0 || i == len(rsName)-1 {
		return "", false
	}
	suffix := rsName[i+1:]
	if len(suffix) < 5 || len(suffix) > 10 {
		return "", false
	}
	for _, r := range suffix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z')) {
			return "", false
		}
	}
	return rsName[:i], true
}
