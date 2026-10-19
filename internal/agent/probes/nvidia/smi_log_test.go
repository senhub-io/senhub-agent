package nvidia

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func collectNTimes(t *testing.T, runErr error, n int) string {
	t.Helper()
	raw, err := NewNvidiaProbe(map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("NewNvidiaProbe: %v", err)
	}
	p := raw.(*NvidiaProbe)
	p.SetName("nvidia-test")
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)
	p.moduleLogger = logger.NewModuleLogger(&base, "probe.nvidia.test")
	p.runSmi = func(string) ([]byte, error) { return nil, runErr }
	for i := 0; i < n; i++ {
		if _, err := p.Collect(); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	return buf.String()
}

func TestMissingNvidiaSmiNeverWarns(t *testing.T) {
	out := collectNTimes(t, fmt.Errorf("running nvidia-smi: %w", exec.ErrNotFound), 3)
	if strings.Contains(out, `"level":"warn"`) {
		t.Fatalf("a host without nvidia-smi must not warn:\n%s", out)
	}
}

func TestFailingNvidiaSmiWarnsOnce(t *testing.T) {
	out := collectNTimes(t, errors.New("driver/library version mismatch"), 3)
	if n := strings.Count(out, `"level":"warn"`); n != 1 {
		t.Fatalf("expected one warn over three failing cycles, got %d:\n%s", n, out)
	}
}
