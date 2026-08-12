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

		// Every pod needs an anchor, and an unscheduled one is the case that
		// matters most.
		//
		// A Pending pod has no nodeName, so it would carry no relation at all
		// and the anti-orphan guard would drop it before the wire — silently
		// removing from the graph exactly the pod an operator is looking for.
		// Measured on a real cluster: a StatefulSet stuck on an unbindable
		// claim produced two pods that never arrived (#761).
		//
		// So the edge targets the node when the pod is placed, and the cluster
		// when it is not. Both are true: a scheduled pod runs on its node, an
		// unscheduled one exists in the cluster and runs nowhere yet.
		if nodeID := p.nodeMachineIDs[pod.Spec.NodeName]; nodeID != "" {
			if rel, ok := runsOn(entity.TypePod, podEnt.ID,
				entity.TypeHost, map[string]any{"host.id": nodeID}); ok {
				p.pendingInventory.relations = append(p.pendingInventory.relations, rel)
			}
		} else if clusterID := p.clusterIdentity(); clusterID != "" {
			if rel, ok := runsOn(entity.TypePod, podEnt.ID, entity.TypeServiceInstance,
				map[string]any{"service.instance.id": clusterID}); ok {
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
