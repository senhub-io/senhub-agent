package kubernetes

import (
	"time"

	corev1 "k8s.io/api/core/v1"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Node conditions beyond Ready, and the resource reservations that say whether
// a cluster is over-committed.
//
// A node under disk pressure is still Ready right up to the moment it is not:
// the kubelet starts evicting pods while the only condition this probe read
// stayed true. The pressure conditions are the early signal, and they cost one
// extra pass over a list already fetched.

// nodePressureConditions are the conditions worth a series. Ready is emitted
// separately by the node collector and is not repeated here.
//
// The value convention is deliberate and opposite to Ready: 1 means the
// pressure IS present, i.e. 1 is bad. Ready uses 1 for good. Mixing the two on
// one dashboard is a real trap, so each metric name says which state it counts
// rather than a neutral "status".
var nodePressureConditions = []struct {
	condition corev1.NodeConditionType
	metric    string
}{
	{corev1.NodeMemoryPressure, "k8s.node.condition.memory_pressure"},
	{corev1.NodeDiskPressure, "k8s.node.condition.disk_pressure"},
	{corev1.NodePIDPressure, "k8s.node.condition.pid_pressure"},
	{corev1.NodeNetworkUnavailable, "k8s.node.condition.network_unavailable"},
}

// nodeConditionPoints emits one series per pressure condition.
//
// A condition the API does not report is emitted as 0 rather than omitted:
// absence would be indistinguishable from "no pressure" on a dashboard, and
// the two mean different things only when you already suspect a problem.
// NetworkUnavailable is the exception — many CNIs never set it, so reporting a
// steady 0 would be inventing a fact. It is emitted only when present.
func nodeConditionPoints(n *corev1.Node, now time.Time, base []tags.Tag) []data_store.DataPoint {
	present := make(map[corev1.NodeConditionType]corev1.ConditionStatus, len(n.Status.Conditions))
	for _, c := range n.Status.Conditions {
		present[c.Type] = c.Status
	}

	var points []data_store.DataPoint
	for _, pc := range nodePressureConditions {
		status, reported := present[pc.condition]
		if !reported {
			if pc.condition == corev1.NodeNetworkUnavailable {
				continue
			}
			points = append(points, point(pc.metric, 0, now, base))
			continue
		}
		value := float64(0)
		if status == corev1.ConditionTrue {
			value = 1
		}
		points = append(points, point(pc.metric, value, now, base))
	}
	return points
}

// podResourcePoints emits the CPU and memory a pod reserves, summed over its
// containers.
//
// Requests are what the scheduler committed; limits are the ceiling before
// throttling or an OOM kill. Without both there is no way to answer whether a
// cluster is over-committed, which is the question asked immediately after an
// outage and impossible to answer retroactively without the series.
//
// Init containers are excluded: they do not hold their reservation for the
// life of the pod, so adding them would overstate the standing commitment.
func podResourcePoints(pod *corev1.Pod, now time.Time, base []tags.Tag) []data_store.DataPoint {
	var reqCPU, reqMem, limCPU, limMem float64
	var anyLimit bool

	for i := range pod.Spec.Containers {
		c := &pod.Spec.Containers[i]
		if v := c.Resources.Requests.Cpu(); v != nil {
			reqCPU += v.AsApproximateFloat64()
		}
		if v := c.Resources.Requests.Memory(); v != nil {
			reqMem += float64(v.Value())
		}
		if v := c.Resources.Limits.Cpu(); v != nil && !v.IsZero() {
			limCPU += v.AsApproximateFloat64()
			anyLimit = true
		}
		if v := c.Resources.Limits.Memory(); v != nil && !v.IsZero() {
			limMem += float64(v.Value())
			anyLimit = true
		}
	}

	points := []data_store.DataPoint{
		point("k8s.pod.cpu.request", reqCPU, now, base),
		point("k8s.pod.memory.request", reqMem, now, base),
	}
	// A pod with no limit set is unbounded, which is a different fact from a
	// limit of zero. Emitting 0 would read as "capped at nothing".
	if anyLimit {
		points = append(points,
			point("k8s.pod.cpu.limit", limCPU, now, base),
			point("k8s.pod.memory.limit", limMem, now, base),
		)
	}
	return points
}

// containerResourcePoints emits the same pair per container, so a single
// greedy container inside an otherwise modest pod is attributable.
func containerResourcePoints(c *corev1.Container, now time.Time, base []tags.Tag) []data_store.DataPoint {
	var points []data_store.DataPoint

	if v := c.Resources.Requests.Cpu(); v != nil {
		points = append(points, point("k8s.container.cpu.request", v.AsApproximateFloat64(), now, base))
	}
	if v := c.Resources.Requests.Memory(); v != nil {
		points = append(points, point("k8s.container.memory.request", float64(v.Value()), now, base))
	}
	if v := c.Resources.Limits.Cpu(); v != nil && !v.IsZero() {
		points = append(points, point("k8s.container.cpu.limit", v.AsApproximateFloat64(), now, base))
	}
	if v := c.Resources.Limits.Memory(); v != nil && !v.IsZero() {
		points = append(points, point("k8s.container.memory.limit", float64(v.Value()), now, base))
	}
	return points
}
