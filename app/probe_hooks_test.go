package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type previewBlockingProbe struct {
	*types.BaseProbe
	entered chan struct{}
	ended   chan error
}

func (p *previewBlockingProbe) ShouldStart() bool                  { return true }
func (p *previewBlockingProbe) GetInterval() time.Duration         { return time.Minute }
func (p *previewBlockingProbe) OnStart(chan struct{}) error        { return nil }
func (p *previewBlockingProbe) OnShutdown(_ context.Context) error { return nil }
func (p *previewBlockingProbe) Collect() ([]datapoint.DataPoint, error) {
	return p.CollectContext(context.Background())
}
func (p *previewBlockingProbe) CollectContext(ctx context.Context) ([]datapoint.DataPoint, error) {
	close(p.entered)
	<-ctx.Done()
	p.ended <- ctx.Err()
	return nil, ctx.Err()
}

type previewPlainProbe struct{ *types.BaseProbe }

func (p *previewPlainProbe) ShouldStart() bool                  { return true }
func (p *previewPlainProbe) GetInterval() time.Duration         { return time.Minute }
func (p *previewPlainProbe) OnStart(chan struct{}) error        { return nil }
func (p *previewPlainProbe) OnShutdown(_ context.Context) error { return nil }
func (p *previewPlainProbe) Collect() ([]datapoint.DataPoint, error) {
	return []datapoint.DataPoint{{Name: "preview.metric", Value: 2, Timestamp: time.Now()}}, nil
}

// A console client that leaves the page cancels the request context;
// the test collection must stop with it rather than keep running against
// the target.
func TestPreviewCollectStopsWhenTheRequestIsAbandoned(t *testing.T) {
	probe := &previewBlockingProbe{
		BaseProbe: &types.BaseProbe{},
		entered:   make(chan struct{}),
		ended:     make(chan error, 1),
	}
	ctor := func(map[string]interface{}, *agentLogger.Logger) (types.Probe, error) { return probe, nil }

	reqCtx, abandon := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := previewCollect(reqCtx, ctor, nil)
		done <- err
	}()
	select {
	case <-probe.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the preview never started collecting")
	}
	abandon()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an abandoned preview reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("the preview did not return after its request was abandoned")
	}
	select {
	case err := <-probe.ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the collection ended with %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the collection kept running after the request was abandoned")
	}
}

func TestPreviewCollectDrivesAPlainProbe(t *testing.T) {
	ctor := func(map[string]interface{}, *agentLogger.Logger) (types.Probe, error) {
		return &previewPlainProbe{BaseProbe: &types.BaseProbe{}}, nil
	}
	metrics, err := previewCollect(context.Background(), ctor, nil)
	if err != nil {
		t.Fatalf("previewCollect: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "preview.metric" || metrics[0].Value != 2 {
		t.Fatalf("got %+v, want the one datapoint the probe returned", metrics)
	}
}
