package otlp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMetricsHealth_BackendDown(t *testing.T) {
	now := time.Unix(10_000, 0)
	h := &metricsHealth{now: func() time.Time { return now }}
	interval := 30 * time.Second

	if h.backendDown(interval) {
		t.Error("a collector never tried must not count as down")
	}
	h.markSuccess()
	if h.backendDown(interval) {
		t.Error("a fresh success must not count as down")
	}
	now = now.Add(time.Second)
	h.markFailure()
	if !h.backendDown(interval) {
		t.Error("a failure after the last success means down")
	}
	now = now.Add(time.Second)
	h.markSuccess()
	if h.backendDown(interval) {
		t.Error("a success after the failure means recovered")
	}
	now = now.Add(4 * interval)
	if !h.backendDown(interval) {
		t.Error("no acknowledged export for several intervals means silent")
	}
}

// What the operator reads at a clean stop: a collector that was already down
// is one Info line; a deadline or failure while it was healthy stays a
// warning.
func TestReportMetricsFinalFlush(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name    string
		down    bool
		ctxErr  error
		err     error
		want    []string
		wantNot []string
		wantErr bool
	}{
		{
			name: "down, deadline", down: true, ctxErr: context.DeadlineExceeded,
			err:     context.DeadlineExceeded,
			want:    []string{`"level":"info"`, "final metrics point not delivered: collector unreachable"},
			wantNot: []string{`"level":"warn"`},
		},
		{
			name: "healthy, deadline", ctxErr: context.DeadlineExceeded,
			err:     context.DeadlineExceeded,
			want:    []string{`"level":"warn"`, "final flush did not finish within its budget"},
			wantNot: []string{"collector unreachable"},
			wantErr: true,
		},
		{
			name: "healthy, real failure", err: boom,
			want:    []string{`"level":"warn"`, "shutdown encountered errors"},
			wantErr: true,
		},
		{
			name: "down, real failure", down: true, err: boom,
			want:    []string{`"level":"warn"`},
			wantNot: []string{"collector unreachable"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		err := reportMetricsFinalFlush(captureLogger(&buf), c.down, 12, 12, 5*time.Second, c.ctxErr, c.err)
		out := buf.String()
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: output %q lacks %q", c.name, out, w)
			}
		}
		for _, w := range c.wantNot {
			if strings.Contains(out, w) {
				t.Errorf("%s: output %q must not contain %q", c.name, out, w)
			}
		}
		if (err != nil) != c.wantErr {
			t.Errorf("%s: returned error %v, want error=%v", c.name, err, c.wantErr)
		}
	}
}
