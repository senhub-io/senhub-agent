package kubernetes

import (
	corev1 "k8s.io/api/core/v1"

	"senhub-agent.go/internal/agent/services/entity"
)

// appendPodEntities records the pod, its containers, and the placement chain
// container → pod → host.
//
// Built from the pod list the metric cycle already walks: the entity rail
// costs no extra API call and cannot describe a different instant from the
// metrics beside it.
func (p *KubernetesProbe) appendPodEntities(pod *corev1.Pod) {
	podEnt, hasPod := podEntity(pod)
	if hasPod {
		p.pendingInventory.entities = append(p.pendingInventory.entities, podEnt)

		// pod → host. Skipped when the node contributed no host entity, which
		// happens by design on a node whose machine-id is unreadable.
		if nodeID := p.nodeMachineIDs[pod.Spec.NodeName]; nodeID != "" {
			if rel, ok := runsOn(entity.TypePod, podEnt.ID,
				entity.TypeHost, map[string]any{"host.id": nodeID}); ok {
				p.pendingInventory.relations = append(p.pendingInventory.relations, rel)
			}
		}
	}

	for i := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[i]
		ent, ok := containerEntity(cs, pod, p.nodeMachineIDs[pod.Spec.NodeName])
		if !ok {
			continue
		}
		p.pendingInventory.entities = append(p.pendingInventory.entities, ent)

		// container → pod. The target is the pod, not the node: the pod is the
		// unit of failure, so a drained node evicts pods and the pods take
		// their containers with them. Falling back to the node when the pod has
		// no UID would produce a chain that skips a level and reports a
		// different impact radius, so it is not done.
		if hasPod {
			if rel, ok := runsOn(entity.TypeContainer, ent.ID, entity.TypePod, podEnt.ID); ok {
				p.pendingInventory.relations = append(p.pendingInventory.relations, rel)
			}
		}
	}
}
