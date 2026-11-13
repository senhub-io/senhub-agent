package kafka

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"senhub-agent.go/internal/agent/probes/types"
)

type closeTrackingAdmin struct {
	sarama.ClusterAdmin
	closed atomic.Bool
}

func (a *closeTrackingAdmin) Close() error {
	a.closed.Store(true)
	return nil
}

// A broker that stops answering during the connection must not hold a
// cancelled collection, and the interruption is not reported as the
// cluster being down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	probe, err := NewKafkaProbe(map[string]interface{}{
		"brokers":       []interface{}{"127.0.0.1:9092"},
		"instance_name": "test-kafka-cluster",
	}, makeLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaProbe: %v", err)
	}
	p := probe.(*kafkaProbe)

	release := make(chan struct{})
	admin := &closeTrackingAdmin{}
	p.newAdmin = func([]string, *sarama.Config) (sarama.ClusterAdmin, error) {
		<-release
		return admin, nil
	}
	p.newClient = func([]string, *sarama.Config) (sarama.Client, error) {
		t.Error("a cancelled cycle went on to open the client")
		return nil, errors.New("unexpected")
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	started := time.Now()
	points, err := p.CollectContext(ctx)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("CollectContext took %v after its context was cancelled", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
	if len(points) != 0 {
		t.Fatalf("an interrupted cycle emitted %d datapoints, want none (no false up=0)", len(points))
	}

	// The dial that outlived the cycle hands back a connection nobody
	// uses any more: it must be closed, not leaked.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for !admin.closed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the admin connection opened after the cancellation was never closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A connection failure with a live context is still a measurement.
func TestCollectContext_ConnectFailureStillReportsDown(t *testing.T) {
	probe, err := NewKafkaProbe(map[string]interface{}{
		"brokers":       []interface{}{"127.0.0.1:9092"},
		"instance_name": "test-kafka-cluster",
	}, makeLogger(t))
	if err != nil {
		t.Fatalf("NewKafkaProbe: %v", err)
	}
	p := probe.(*kafkaProbe)
	p.newAdmin = func([]string, *sarama.Config) (sarama.ClusterAdmin, error) {
		return nil, errors.New("connection refused")
	}

	points, err := p.CollectContext(context.Background())
	if err != nil {
		t.Fatalf("a connection failure is a measurement, got error %v", err)
	}
	if len(points) == 0 {
		t.Fatal("no datapoints for a failed connection, want senhub.kafka.up=0")
	}
}

// The poller detects cancellation support by type assertion; a lost
// method would silently fall back to the uncancellable Collect.
var _ types.ContextCollector = (*kafkaProbe)(nil)
