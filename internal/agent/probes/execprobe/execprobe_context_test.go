package execprobe

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

var _ types.ContextCollector = (*ExecProbe)(nil)

// The probe's own timeout still applies under a context that outlasts it,
// and stays a measurement rather than a collection error.
func TestCollectContext_OwnTimeoutShorterThanTheContextStaysAMeasurement(t *testing.T) {
	script := writeScript(t, "sleep 30\n", "@ping -n 31 127.0.0.1 >nul\r\n")
	p := newTestProbe(t, map[string]interface{}{"command": script, "timeout": 1})

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	points, err := p.CollectContext(ctx)
	if err != nil {
		t.Fatalf("own timeout must not be a collection error: %v", err)
	}
	got := map[string]float64{}
	for _, dp := range points {
		got[dp.Name] = dp.Value
	}
	if got["senhub.exec.timeout"] != 1 || got["senhub.exec.status"] != 3 {
		t.Errorf("timeout/status = %v/%v, want 1/3", got["senhub.exec.timeout"], got["senhub.exec.status"])
	}
}
