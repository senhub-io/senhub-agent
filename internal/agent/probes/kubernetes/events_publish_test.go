package kubernetes

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
)

// The test that was missing, and that the lab run showed was needed: the
// earlier event tests all checked the SHAPE of a record built in isolation.
// None exercised collectEvents, so nothing verified that a record actually
// reaches the log bus — which on a real cluster it did not appear to.
func TestCollectEvents_PublishesNewEventsOnTheSecondCycle(t *testing.T) {
	sub := agentstate.SubscribeLogsFor("otlp", 64)

	base := time.Now().Add(-time.Minute)
	first := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "default"},
		Type:           corev1.EventTypeWarning,
		Reason:         "FailedScheduling",
		Message:        "0/5 nodes are available",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api"},
		LastTimestamp:  metav1.NewTime(base),
	}

	p := testProbe(first)
	p.BaseProbe = &types.BaseProbe{}
	p.moduleLogger = logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "probe.kubernetes.test")
	p.cfg.CollectEvents = true

	// First cycle only arms the cursor: replaying the API's retention window
	// would deliver an hour of past incidents as though they were happening
	// now.
	if err := p.collectEvents(context.Background(), time.Now()); err != nil {
		t.Fatalf("first cycle: %v", err)
	}
	select {
	case rec := <-sub:
		t.Fatalf("first cycle published %q; it must only arm the cursor", rec.Body)
	case <-time.After(200 * time.Millisecond):
	}

	// A genuinely newer event must now be published.
	newer := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "e2", Namespace: "default"},
		Type:           corev1.EventTypeWarning,
		Reason:         "BackOff",
		Message:        "Back-off pulling image",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api"},
		LastTimestamp:  metav1.NewTime(base.Add(30 * time.Second)),
	}
	if _, err := p.clientset.CoreV1().Events("default").Create(
		context.Background(), newer, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating the newer event: %v", err)
	}

	if err := p.collectEvents(context.Background(), time.Now()); err != nil {
		t.Fatalf("second cycle: %v", err)
	}

	select {
	case rec := <-sub:
		if rec.Body != "Back-off pulling image" {
			t.Errorf("published body = %q, want the message verbatim", rec.Body)
		}
		if rec.Attributes["k8s.pod.name"] != "api" {
			t.Errorf("record carries no k8s.pod.name; it cannot be joined to the "+
				"pod's series: %v", rec.Attributes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no record reached the log bus on the second cycle — the events " +
			"rail publishes nothing, which is what the lab cluster showed")
	}
}
