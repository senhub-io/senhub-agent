package kubernetes

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

// Workload kinds beyond Deployment.
//
// A cluster running only StatefulSets was invisible to this probe: it
// collected nodes, pods, containers and deployments, so a database cluster,
// a DaemonSet-deployed log shipper or a failing CronJob produced no series at
// all. Coverage here follows k8sclusterreceiver so an operator comparing our
// output to theirs finds the same names.
//
// Every kind emits the same shape — desired against actual — because that
// difference is the question an operator asks during a rollout, and it is the
// one a single count cannot answer.

// collectStatefulSets emits desired/ready/current/updated replica counts.
func (p *KubernetesProbe) collectStatefulSets(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing statefulsets in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			s := &list.Items[i]
			base := p.workloadTags("statefulset", s.Namespace, s.Name)

			desired := int32(0)
			if s.Spec.Replicas != nil {
				desired = *s.Spec.Replicas
			}
			points = append(points,
				point("k8s.statefulset.desired", float64(desired), now, base),
				point("k8s.statefulset.ready", float64(s.Status.ReadyReplicas), now, base),
				point("k8s.statefulset.current", float64(s.Status.CurrentReplicas), now, base),
				point("k8s.statefulset.updated", float64(s.Status.UpdatedReplicas), now, base),
			)
		}
	}
	return points, firstErr
}

// collectDaemonSets emits the scheduling picture: a DaemonSet is healthy when
// ready equals desired-scheduled, and the gap names how many nodes are not
// running it.
func (p *KubernetesProbe) collectDaemonSets(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing daemonsets in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			d := &list.Items[i]
			base := p.workloadTags("daemonset", d.Namespace, d.Name)
			points = append(points,
				point("k8s.daemonset.desired_scheduled", float64(d.Status.DesiredNumberScheduled), now, base),
				point("k8s.daemonset.current_scheduled", float64(d.Status.CurrentNumberScheduled), now, base),
				point("k8s.daemonset.ready", float64(d.Status.NumberReady), now, base),
				point("k8s.daemonset.misscheduled", float64(d.Status.NumberMisscheduled), now, base),
			)
		}
	}
	return points, firstErr
}

// collectReplicaSets emits desired/ready/available.
//
// ReplicaSets a Deployment owns are mostly noise, but a rollout leaves the
// previous ReplicaSet alive and non-zero — which is exactly how a stuck
// rollout looks from the outside.
func (p *KubernetesProbe) collectReplicaSets(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing replicasets in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			r := &list.Items[i]
			base := p.workloadTags("replicaset", r.Namespace, r.Name)

			desired := int32(0)
			if r.Spec.Replicas != nil {
				desired = *r.Spec.Replicas
			}
			points = append(points,
				point("k8s.replicaset.desired", float64(desired), now, base),
				point("k8s.replicaset.ready", float64(r.Status.ReadyReplicas), now, base),
				point("k8s.replicaset.available", float64(r.Status.AvailableReplicas), now, base),
			)
		}
	}
	return points, firstErr
}

// collectJobs emits active/succeeded/failed pod counts per Job.
//
// failed is the one that matters: a Job whose pods keep failing stays present
// and looks scheduled, and nothing in the pod metrics says the Job as a whole
// is not converging.
func (p *KubernetesProbe) collectJobs(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing jobs in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			j := &list.Items[i]
			base := p.workloadTags("job", j.Namespace, j.Name)
			points = append(points,
				point("k8s.job.active", float64(j.Status.Active), now, base),
				point("k8s.job.succeeded", float64(j.Status.Succeeded), now, base),
				point("k8s.job.failed", float64(j.Status.Failed), now, base),
			)
			if j.Spec.Completions != nil {
				points = append(points,
					point("k8s.job.desired_completions", float64(*j.Spec.Completions), now, base))
			}
		}
	}
	return points, firstErr
}

// collectCronJobs emits the count of currently-active Jobs, and whether the
// schedule is suspended.
//
// A suspended CronJob is the quiet failure: nothing runs, nothing errors, and
// the absence of Jobs looks identical to a schedule that simply has not fired.
func (p *KubernetesProbe) collectCronJobs(ctx context.Context, now time.Time) ([]data_store.DataPoint, error) {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var points []data_store.DataPoint
	var firstErr error
	for _, ns := range namespaces {
		list, err := p.clientset.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing cronjobs in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			c := &list.Items[i]
			base := p.workloadTags("cronjob", c.Namespace, c.Name)
			points = append(points,
				point("k8s.cronjob.active_jobs", float64(len(c.Status.Active)), now, base))

			suspended := float64(0)
			if c.Spec.Suspend != nil && *c.Spec.Suspend {
				suspended = 1
			}
			points = append(points, point("k8s.cronjob.suspended", suspended, now, base))
		}
	}
	return points, firstErr
}

// workloadTags is the tag set every workload metric carries. The kind rides
// as a tag rather than in the metric name so a dashboard can group across
// kinds without knowing the list.
func (p *KubernetesProbe) workloadTags(kind, namespace, name string) []tags.Tag {
	return []tags.Tag{
		{Key: "k8s.cluster.name", Value: p.clusterEndpoint},
		{Key: "k8s.namespace.name", Value: namespace},
		{Key: "k8s." + kind + ".name", Value: name},
		{Key: "k8s.workload.kind", Value: kind},
		{Key: "k8s.workload.name", Value: name},
		{Key: "metric_type", Value: "workload"},
	}
}

// point is the datapoint constructor these collectors share; it exists to keep
// the emission sites readable, since the interesting part of each is the value
// being read from the API object, not the struct literal around it.
func point(name string, value float64, now time.Time, t []tags.Tag) data_store.DataPoint {
	return data_store.DataPoint{Name: name, Value: value, Timestamp: now, Tags: t}
}

// interface assertions so a client-go version bump that changes these shapes
// fails here rather than at the first call against a real cluster.
var (
	_ = (*appsv1.StatefulSet)(nil)
	_ = (*appsv1.DaemonSet)(nil)
	_ = (*appsv1.ReplicaSet)(nil)
	_ = (*batchv1.Job)(nil)
	_ = (*batchv1.CronJob)(nil)
)
