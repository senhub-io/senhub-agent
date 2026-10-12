package otlp

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLogsQueueAnnounce_StatesPathCapAndRetention(t *testing.T) {
	var buf bytes.Buffer
	dir := t.TempDir()
	q := newLogsQueue(dir, 64<<20, captureLogger(&buf))
	q.setMaxAge(12 * time.Hour)
	q.announce()

	out := buf.String()
	for _, want := range []string{`"level":"info"`, "OTLP logs queue active", dir, `"max_bytes":67108864`, `"retention":43200000`} {
		if !strings.Contains(out, want) {
			t.Errorf("announcement lacks %q: %s", want, out)
		}
	}
	if n := strings.Count(out, "OTLP logs queue active"); n != 1 {
		t.Errorf("announced %d times, want 1", n)
	}
}

func TestLogsQueueAnnounce_DefaultCapAndNoAgeLimit(t *testing.T) {
	var buf bytes.Buffer
	q := newLogsQueue(t.TempDir(), 0, captureLogger(&buf))
	q.announce()
	out := buf.String()
	if !strings.Contains(out, `"max_bytes":134217728`) || !strings.Contains(out, `"retention":"unlimited"`) {
		t.Errorf("default cap or retention missing: %s", out)
	}
}
