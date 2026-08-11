package kubernetes

import (
	"context"
	"fmt"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Storage, quotas and autoscaling — the three views of "does this cluster
// still have room".
//
// They are grouped because they fail together and are read together: a
// deployment that will not scale is either out of quota, out of nodes, or
// waiting on a volume that will never bind, and telling those apart is the
// whole diagnostic.

// phaseValue maps a lifecycle phase to a one-hot metric per phase.
//
// A phase is a string, and a string cannot be a metric value. Emitting one
// series per phase with 0/1 lets a dashboard sum "how many claims are
// Pending" without decoding an enum, and it survives a new phase being added
// upstream — an unknown phase simply lights none of the known series rather
// than silently mapping onto one of them.
func phaseSeries(prefix string, current string, known []string, now time.Time, base []tags.Tag) []data_store.DataPoint {
	points := make([]data_store.DataPoint, 0, len(known))
	for _, phase := range known {
		v := float64(0)
		if phase == current {
			v = 1
		}
		t := append(append([]tags.Tag{}, base...), tags.Tag{Key: "phase", Value: phase})
		points = append(points, point(prefix, v, now, t))
	}
	return points
}

var pvPhases = []string{"Available", "Bound", "Released", "Failed", "Pending"}
var pvcPhases = []string{"Pending", "Bound", "Lost"}

// collectPersistentVolumes emits capacity and phase per volume.
//
// PersistentVolumes are cluster-scoped, so this ignores the namespace filter:
// a volume is not in a namespace, and pretending otherwise would hide the
// ones bound to claims in excluded namespaces while their capacity still
// counts against the cluster.
func (p *KubernetesProbe) collectPersistentVolumes(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	list, err := p.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing persistentvolumes: %w", err)
	}

	var points []data_store.DataPoint
	for i := range list.Items {
		pv := &list.Items[i]
		base := []tags.Tag{
			{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
			{Key: "k8s.persistentvolume.name", Value: pv.Name},
			{Key: "k8s.storageclass.name", Value: pv.Spec.StorageClassName},
			{Key: "metric_type", Value: "storage"},
		}
		if cap := pv.Spec.Capacity.Storage(); cap != nil {
			points = append(points, point("k8s.persistentvolume.capacity", float64(cap.Value()), now, base))
		}
		points = append(points, phaseSeries("k8s.persistentvolume.phase",
			string(pv.Status.Phase), pvPhases, now, base)...)
	}
	return points, nil
}

// collectPersistentVolumeClaims emits requested capacity and phase per claim.
//
// A claim stuck Pending is the failure worth catching: the pod that wants it
// stays Pending too, and from the pod's own metrics that is indistinguishable
// from a cluster simply out of CPU.
func (p *KubernetesProbe) collectPersistentVolumeClaims(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing persistentvolumeclaims in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			pvc := &list.Items[i]
			base := []tags.Tag{
				{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
				{Key: "k8s.namespace.name", Value: pvc.Namespace},
				{Key: "k8s.persistentvolumeclaim.name", Value: pvc.Name},
				{Key: "metric_type", Value: "storage"},
			}
			if req := pvc.Spec.Resources.Requests.Storage(); req != nil {
				points = append(points, point("k8s.persistentvolumeclaim.requested", float64(req.Value()), now, base))
			}
			// Capacity is what was actually granted; it can exceed the request
			// when the storage class rounds up, and it is absent while Pending.
			if cap := pvc.Status.Capacity.Storage(); cap != nil && !cap.IsZero() {
				points = append(points, point("k8s.persistentvolumeclaim.capacity", float64(cap.Value()), now, base))
			}
			points = append(points, phaseSeries("k8s.persistentvolumeclaim.phase",
				string(pvc.Status.Phase), pvcPhases, now, base)...)
		}
	}
	return points, firstErr
}

// collectResourceQuotas emits hard limit and current use per resource.
//
// The pair is the point: a quota at 99 % is the reason a deployment will not
// scale, and neither number alone says so.
func (p *KubernetesProbe) collectResourceQuotas(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.CoreV1().ResourceQuotas(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing resourcequotas in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			q := &list.Items[i]
			for name, hard := range q.Status.Hard {
				base := []tags.Tag{
					{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
					{Key: "k8s.namespace.name", Value: q.Namespace},
					{Key: "k8s.resourcequota.name", Value: q.Name},
					{Key: "k8s.resourcequota.resource", Value: string(name)},
					{Key: "metric_type", Value: "quota"},
				}
				points = append(points, point("k8s.resourcequota.hard",
					quantityValue(name, hard), now, base))
				if used, ok := q.Status.Used[name]; ok {
					points = append(points, point("k8s.resourcequota.used",
						quantityValue(name, used), now, base))
				}
			}
		}
	}
	return points, firstErr
}

// collectHorizontalPodAutoscalers emits current, desired, min and max
// replicas.
//
// An HPA pinned at max is the signal that the cluster is refusing to grow
// further, and it is invisible from the workload's own replica counts, which
// look perfectly healthy at whatever ceiling they have hit.
func (p *KubernetesProbe) collectHorizontalPodAutoscalers(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing horizontalpodautoscalers in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			h := &list.Items[i]
			base := []tags.Tag{
				{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
				{Key: "k8s.namespace.name", Value: h.Namespace},
				{Key: "k8s.hpa.name", Value: h.Name},
				{Key: "k8s.hpa.target", Value: h.Spec.ScaleTargetRef.Name},
				{Key: "metric_type", Value: "autoscaling"},
			}
			points = append(points,
				point("k8s.hpa.current_replicas", float64(h.Status.CurrentReplicas), now, base),
				point("k8s.hpa.desired_replicas", float64(h.Status.DesiredReplicas), now, base),
				point("k8s.hpa.max_replicas", float64(h.Spec.MaxReplicas), now, base),
			)
			if h.Spec.MinReplicas != nil {
				points = append(points, point("k8s.hpa.min_replicas", float64(*h.Spec.MinReplicas), now, base))
			}
		}
	}
	return points, firstErr
}

// quantityValue reads a quota quantity in the unit that matches the resource.
//
// A ResourceQuota mixes units in one map: cpu is in cores, memory and storage
// in bytes, and counts (pods, services, secrets) are plain integers. Reading
// them all one way would report memory as a fractional core count or cpu as a
// byte figure — both plausible-looking and wrong, which is the worst kind.
func quantityValue(name corev1.ResourceName, q resource.Quantity) float64 {
	switch name {
	case corev1.ResourceCPU, corev1.ResourceLimitsCPU, corev1.ResourceRequestsCPU:
		return q.AsApproximateFloat64()
	default:
		return float64(q.Value())
	}
}

var _ = (*autoscalingv2.HorizontalPodAutoscaler)(nil)
