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

// nodeEntity turns a Kubernetes node into a host entity, or reports that it
// must not be emitted.
//
// The identity is the node's MachineID, which the kubelet reads from
// /etc/machine-id — the same file gopsutil reads for the agent's own host.id.
// A node entity keyed on it is therefore byte-identical to the entity an agent
// running INSIDE that node emits for itself, and the two converge on one graph
// node instead of duplicating. That convergence is the point of the exercise,
// not a side effect: two observers deriving identical ids for the same thing
// must land on one node (ADR 0018).
//
// When the MachineID is absent, NOTHING is emitted. This is the consumer's
// instruction and it is the right one: a missing node is a visible hole that
// an in-guest agent fills, while a node keyed on a fallback source is a silent
// duplicate nobody will ever notice. The same ordering rule as everywhere else
// — better a gap the consumer can see than an answer it cannot check — applied
// this time to the SOURCE of the identity rather than to its publication.
//
// SystemUUID is carried as a descriptive attribute rather than used as the
// fallback: it is absent or forged on several virtualisation platforms, and it
// is the natural same_as facet the day the consumer implements that overlay.
func nodeEntity(n *corev1.Node, clusterUID string) (entity.Entity, bool) {
	machineID := strings.TrimSpace(n.Status.NodeInfo.MachineID)
	if machineID == "" {
		return entity.Entity{}, false
	}

	attrs := map[string]any{
		"host.name":             n.Name,
		"k8s.node.name":         n.Name,
		"os.type":               n.Status.NodeInfo.OperatingSystem,
		"host.arch":             n.Status.NodeInfo.Architecture,
		"k8s.kubelet.version":   n.Status.NodeInfo.KubeletVersion,
		"k8s.node.os_image":     n.Status.NodeInfo.OSImage,
		"k8s.container.runtime": n.Status.NodeInfo.ContainerRuntimeVersion,
	}
	if uuid := strings.TrimSpace(n.Status.NodeInfo.SystemUUID); uuid != "" {
		attrs["host.uuid"] = uuid
	}
	if clusterUID != "" {
		attrs["k8s.cluster.uid"] = clusterUID
	}

	return entity.Entity{
		Type:       entity.TypeHost,
		ID:         map[string]any{"host.id": machineID},
		Attributes: attrs,
	}, true
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

// containerRunsOnNode is the container's placement edge.
//
// The target is the NODE today and becomes the POD once the consumer publishes
// that type: container → runs_on → pod → runs_on → host. They confirmed that
// is an edge change rather than a re-key, so this can ship now and move later
// without disturbing any identity.
//
// runs_on propagates failure from target to source, which is the semantics
// wanted here: a node going down takes its containers with it, and an impact
// query answers transitively without extra code.
func containerRunsOnNode(containerID, nodeMachineID string) (entity.Relation, bool) {
	if containerID == "" || nodeMachineID == "" {
		return entity.Relation{}, false
	}
	return entity.Relation{
		Type:     entity.RelRunsOn,
		FromType: entity.TypeContainer,
		FromID:   map[string]any{"container.id": containerID},
		ToType:   entity.TypeHost,
		ToID:     map[string]any{"host.id": nodeMachineID},
	}, true
}
