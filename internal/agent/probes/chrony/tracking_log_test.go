package chrony

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func collectWithError(t *testing.T, runErr error, n int) string {
	t.Helper()
	raw, err := NewChronyProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewChronyProbe: %v", err)
	}
	p := raw.(*ChronyProbe)
	p.SetName("chrony-test")
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)
	p.moduleLogger = logger.NewModuleLogger(&base, "probe.chrony.test")
	p.run = func() trackingResult { return trackingResult{err: runErr} }
	for i := 0; i < n; i++ {
		if _, err := p.Collect(); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	return buf.String()
}

func TestHostWithoutChronyNeverWarns(t *testing.T) {
	out := collectWithError(t, errors.New(`exec: "chronyc": executable file not found in $PATH`), 3)
	if strings.Contains(out, `"level":"warn"`) {
		t.Fatalf("a host without chrony must not warn:\n%s", out)
	}
}

func TestUnreadableChronyWarnsOnce(t *testing.T) {
	out := collectWithError(t, errors.New("chronyc tracking: exit status 1"), 3)
	if n := strings.Count(out, `"level":"warn"`); n != 1 {
		t.Fatalf("expected one warn over three failing cycles, got %d:\n%s", n, out)
	}
}
