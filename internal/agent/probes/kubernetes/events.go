package kubernetes

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// Kubernetes Events on the log rail.
//
// Events are the cluster's own journal: "Failed to pull image", "node had
// insufficient memory", "0/5 nodes are available: 5 Insufficient cpu",
// "Liveness probe failed". They are the only place Kubernetes explains WHY a
// metric moved, and until now the probe collected none of them.
//
// They ride the log rail rather than becoming metrics, because that is what
// they are: timestamped, textual, unbounded in cardinality. Turning "0/5
// nodes are available: 5 Insufficient cpu" into a counter would keep the
// count and throw away the sentence, which is the part an operator reads.

// eventSeverity maps the Kubernetes event type to a log severity.
//
// Kubernetes has exactly two types, Normal and Warning, and no error level:
// a failed image pull, a failed mount and an OOM kill are all "Warning". So
// Warning maps to Error rather than Warn — a Kubernetes Warning is an
// operational failure, not a caution, and mapping it to Warn would bury it
// under the routine noise of every other component's warnings.
func eventSeverity(eventType string) (agentstate.LogSeverity, string) {
	switch eventType {
	case corev1.EventTypeWarning:
		return agentstate.LogSeverityError, "ERROR"
	default:
		return agentstate.LogSeverityInfo, "INFO"
	}
}

// collectEvents publishes cluster events emitted since the last cycle.
//
// Only new events are published. The API returns the full window on every
// list (events live an hour by default), so republishing everything each
// cycle would multiply every message by the number of cycles in that hour —
// roughly sixty at a one-minute interval. The cursor is the event's last
// occurrence timestamp.
func (p *KubernetesProbe) collectEvents(ctx context.Context, now time.Time) error {
	namespaces, err := p.resolveNamespaces(ctx)
	if err != nil {
		return err
	}

	cutoff := p.lastEventTime.Load()
	newest := cutoff
	var firstErr error
	published := 0

	for _, ns := range namespaces {
		list, err := p.clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("listing events in %s: %w", ns, err)
			}
			continue
		}
		for i := range list.Items {
			ev := &list.Items[i]
			ts := eventTime(ev)
			unix := ts.UnixNano()

			// On the first cycle the cursor is zero and the whole retention
			// window would be replayed — an hour of history arriving at once,
			// timestamped in the past, which reads as a burst of incidents
			// that already happened. The first cycle only sets the cursor.
			if cutoff == 0 || unix <= cutoff {
				if unix > newest {
					newest = unix
				}
				continue
			}
			if unix > newest {
				newest = unix
			}

			agentstate.PublishLog(p.eventRecord(ev, ts))
			published++
		}
	}

	if newest > cutoff {
		p.lastEventTime.Store(newest)
	}
	if cutoff == 0 {
		p.moduleLogger.Info().
			Msg("kubernetes: event cursor initialised; events from now on are published, the retention window already in the API is not replayed")
	}
	// Info, not Debug: without this line a rail publishing nothing and a rail
	// publishing normally are indistinguishable from outside, so "no events
	// arrived" cannot be told from "no events happened" without a debug build.
	p.moduleLogger.Info().
		Int("published", published).
		Int("namespaces", len(namespaces)).
		Int64("cursor_unix_nano", newest).
		Msg("kubernetes: event cycle complete")
	return firstErr
}

// eventRecord turns an Event into a log record.
//
// The body is the message verbatim. The involved object rides as attributes
// so a reader can pivot from "why did this pod restart" to the pod's metrics
// without parsing the sentence — which is the whole reason this is on the log
// rail next to the metrics rather than in a separate tool.
func (p *KubernetesProbe) eventRecord(ev *corev1.Event, ts time.Time) agentstate.LogRecord {
	severity, severityText := eventSeverity(ev.Type)

	attrs := map[string]string{
		"k8s.cluster.name":      p.clusterEndpoint,
		"k8s.namespace.name":    ev.Namespace,
		"k8s.event.reason":      ev.Reason,
		"k8s.event.type":        ev.Type,
		"k8s.event.object.kind": ev.InvolvedObject.Kind,
		"k8s.event.object.name": ev.InvolvedObject.Name,
		"k8s.event.source":      eventSource(ev),
		"k8s.event.count":       fmt.Sprintf("%d", ev.Count),
	}
	if p.clusterUID != "" {
		attrs["k8s.cluster.uid"] = p.clusterUID
	}
	// The involved object's own name under its kind's key, so an event about a
	// pod carries k8s.pod.name and joins that pod's metric series directly.
	if key := involvedObjectKey(ev.InvolvedObject.Kind); key != "" {
		attrs[key] = ev.InvolvedObject.Name
	}

	return agentstate.LogRecord{
		Timestamp:         ts,
		Severity:          severity,
		SeverityText:      severityText,
		Body:              ev.Message,
		Attributes:        attrs,
		ProducerProbeName: p.GetName(),
		ProducerProbeType: "kubernetes",
		TargetStrategies:  p.GetTargetStrategies(),
	}
}

// involvedObjectKey maps an object kind to the tag its metrics already carry,
// so an event and the series it explains share a label.
//
// Kinds with no metric counterpart return "": inventing k8s.endpoints.name
// would add a label nothing joins on, which is worse than none.
func involvedObjectKey(kind string) string {
	switch kind {
	case "Pod":
		return "k8s.pod.name"
	case "Node":
		return "k8s.node.name"
	case "Deployment":
		return "k8s.deployment.name"
	case "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob":
		return "k8s.workload.name"
	case "PersistentVolumeClaim":
		return "k8s.persistentvolumeclaim.name"
	case "HorizontalPodAutoscaler":
		return "k8s.hpa.name"
	default:
		return ""
	}
}

// eventSource names the component that reported the event — kubelet,
// scheduler, controller-manager. It is what separates "the scheduler could
// not place this" from "the kubelet could not start it", two very different
// problems that produce similarly worded messages.
func eventSource(ev *corev1.Event) string {
	if ev.Source.Component != "" {
		if ev.Source.Host != "" {
			return ev.Source.Component + "@" + ev.Source.Host
		}
		return ev.Source.Component
	}
	if ev.ReportingController != "" {
		return ev.ReportingController
	}
	return "unknown"
}

// eventTime is the event's most recent occurrence.
//
// Kubernetes carries three timestamps and they are not interchangeable:
// LastTimestamp is the legacy repeat marker, EventTime the newer one, and
// only FirstTimestamp is always set. Using the wrong one makes a repeating
// event look like it happened once, an hour ago — so it would fall below the
// cursor forever and never be published again.
func eventTime(ev *corev1.Event) time.Time {
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.EventTime.IsZero() {
		return ev.EventTime.Time
	}
	if !ev.FirstTimestamp.IsZero() {
		return ev.FirstTimestamp.Time
	}
	return time.Now()
}

// eventCursor is the last published event time, in Unix nanoseconds.
// atomic because Collect may overlap a slow cycle with the next tick.
type eventCursor = atomic.Int64
