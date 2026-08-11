package kubernetes

import (
	corev1 "k8s.io/api/core/v1"
)

// appendContainerEntities records one container entity per running container
// of the pod, plus its placement edge.
//
// Built from the pod list the metric cycle already walks — the entity rail
// costs no extra API call, and cannot describe a different instant from the
// metrics beside it.
func (p *KubernetesProbe) appendContainerEntities(pod *corev1.Pod) {
	nodeID := p.nodeMachineIDs[pod.Spec.NodeName]

	for i := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[i]
		ent, ok := containerEntity(cs, pod, nodeID)
		if !ok {
			continue
		}
		p.pendingInventory.entities = append(p.pendingInventory.entities, ent)

		// The edge is skipped when the node contributed no host entity — a
		// relation whose target does not materialise is buffered and then
		// dropped by the consumer, so emitting it would cost a warning and
		// buy nothing.
		if rel, ok := containerRunsOnNode(ent.ID["container.id"].(string), nodeID); ok {
			p.pendingInventory.relations = append(p.pendingInventory.relations, rel)
		}
	}
}
