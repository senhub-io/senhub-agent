package kubernetes

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
)

// A Kubernetes Warning is an operational failure, not a caution: a failed
// image pull, a failed mount and an OOM kill all arrive as "Warning". Mapping
// it to Warn would bury it under every other component's routine warnings.
func TestEventSeverity_WarningIsAnError(t *testing.T) {
	sev, text := eventSeverity(corev1.EventTypeWarning)
	if sev != agentstate.LogSeverityError {
		t.Errorf("Warning mapped to %v, want Error", sev)
	}
	if text != "ERROR" {
		t.Errorf("severity text = %q, want ERROR", text)
	}
	if sev, _ := eventSeverity(corev1.EventTypeNormal); sev != agentstate.LogSeverityInfo {
		t.Errorf("Normal mapped to %v, want Info", sev)
	}
}

// Kubernetes carries three timestamps and they are not interchangeable.
// Reading the wrong one makes a repeating event look like it happened once an
// hour ago — so it stays below the cursor forever and is never published.
func TestEventTime_PrefersTheMostRecentOccurrence(t *testing.T) {
	first := time.Now().Add(-time.Hour)
	last := time.Now().Add(-time.Minute)

	ev := &corev1.Event{
		FirstTimestamp: metav1.NewTime(first),
		LastTimestamp:  metav1.NewTime(last),
	}
	if got := eventTime(ev); !got.Equal(last) {
		t.Errorf("eventTime = %v, want the last occurrence %v", got, last)
	}

	// Newer API objects set EventTime instead of LastTimestamp.
	ev2 := &corev1.Event{
		FirstTimestamp: metav1.NewTime(first),
		EventTime:      metav1.NewMicroTime(last),
	}
	if got := eventTime(ev2); !got.Equal(last) {
		t.Errorf("eventTime with EventTime set = %v, want %v", got, last)
	}

	// Only FirstTimestamp set: it is the only one always populated.
	ev3 := &corev1.Event{FirstTimestamp: metav1.NewTime(first)}
	if got := eventTime(ev3); !got.Equal(first) {
		t.Errorf("eventTime with only FirstTimestamp = %v, want %v", got, first)
	}
}

// An event must carry the label its subject's metrics already carry, so a
// reader can go from "why did this pod restart" to that pod's series without
// parsing the sentence.
func TestInvolvedObjectKey_JoinsTheSubjectsMetrics(t *testing.T) {
	for kind, want := range map[string]string{
		"Pod":                   "k8s.pod.name",
		"Node":                  "k8s.node.name",
		"Deployment":            "k8s.deployment.name",
		"StatefulSet":           "k8s.workload.name",
		"CronJob":               "k8s.workload.name",
		"PersistentVolumeClaim": "k8s.persistentvolumeclaim.name",
	} {
		if got := involvedObjectKey(kind); got != want {
			t.Errorf("involvedObjectKey(%q) = %q, want %q", kind, got, want)
		}
	}

	// A kind with no metric counterpart must return nothing: inventing a label
	// that joins to no series is worse than emitting none.
	if got := involvedObjectKey("Endpoints"); got != "" {
		t.Errorf("involvedObjectKey(Endpoints) = %q, want empty", got)
	}
}

// The source separates "the scheduler could not place this" from "the kubelet
// could not start it" — two different problems with similar wording.
func TestEventSource_NamesTheReportingComponent(t *testing.T) {
	ev := &corev1.Event{Source: corev1.EventSource{Component: "kubelet", Host: "node-1"}}
	if got := eventSource(ev); got != "kubelet@node-1" {
		t.Errorf("eventSource = %q, want kubelet@node-1", got)
	}

	ev2 := &corev1.Event{Source: corev1.EventSource{Component: "default-scheduler"}}
	if got := eventSource(ev2); got != "default-scheduler" {
		t.Errorf("eventSource = %q, want default-scheduler", got)
	}

	ev3 := &corev1.Event{ReportingController: "deployment-controller"}
	if got := eventSource(ev3); got != "deployment-controller" {
		t.Errorf("eventSource = %q, want deployment-controller", got)
	}

	if got := eventSource(&corev1.Event{}); got != "unknown" {
		t.Errorf("eventSource with nothing set = %q, want unknown", got)
	}
}

// The record must carry the message verbatim: the count is not the point, the
// sentence is. "0/5 nodes are available: 5 Insufficient cpu" reduced to a
// counter keeps the number and throws away the diagnosis.
func TestEventRecord_CarriesTheMessageAndTheJoin(t *testing.T) {
	p := &KubernetesProbe{
		BaseProbe:       &types.BaseProbe{},
		clusterEndpoint: "https://api.test",
		clusterUID:      "uid-1",
	}
	ts := time.Now()
	ev := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "prod"},
		Type:           corev1.EventTypeWarning,
		Reason:         "FailedScheduling",
		Message:        "0/5 nodes are available: 5 Insufficient cpu",
		Count:          7,
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-abc"},
		Source:         corev1.EventSource{Component: "default-scheduler"},
	}

	rec := p.eventRecord(ev, ts)

	if rec.Body != ev.Message {
		t.Errorf("body = %q, want the message verbatim", rec.Body)
	}
	if rec.Severity != agentstate.LogSeverityError {
		t.Errorf("severity = %v, want Error for a Warning event", rec.Severity)
	}
	for k, want := range map[string]string{
		"k8s.pod.name":       "api-abc",
		"k8s.namespace.name": "prod",
		"k8s.event.reason":   "FailedScheduling",
		"k8s.event.source":   "default-scheduler",
		"k8s.event.count":    "7",
		"k8s.cluster.uid":    "uid-1",
	} {
		if got := rec.Attributes[k]; got != want {
			t.Errorf("attribute %s = %q, want %q", k, got, want)
		}
	}
	if rec.ProducerProbeType != "kubernetes" {
		t.Errorf("producer type = %q, want kubernetes", rec.ProducerProbeType)
	}
}
