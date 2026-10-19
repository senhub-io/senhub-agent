package subcollect

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func newTestLogger(buf *bytes.Buffer) *logger.ModuleLogger {
	base := zerolog.New(buf).Level(zerolog.DebugLevel)
	return logger.NewModuleLogger(&base, "probe.test")
}

func levels(buf *bytes.Buffer) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.Contains(line, `"level":"warn"`):
			out = append(out, "warn")
		case strings.Contains(line, `"level":"info"`):
			out = append(out, "info")
		case strings.Contains(line, `"level":"debug"`):
			out = append(out, "debug")
		}
	}
	return out
}

func TestFailureWarnsOnceThenDebugThenRecovers(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)
	var tr Tracker
	err := errors.New("boom")

	tr.Failed(log, Warn, "stats", err, "stats failed")
	tr.Failed(log, Warn, "stats", err, "stats failed")
	tr.Failed(log, Warn, "stats", err, "stats failed")
	tr.Succeeded(log, "stats", "stats recovered")
	tr.Succeeded(log, "stats", "stats recovered")
	tr.Failed(log, Warn, "stats", err, "stats failed")

	got := strings.Join(levels(&buf), ",")
	want := "warn,debug,debug,info,warn"
	if got != want {
		t.Fatalf("levels = %s, want %s\n%s", got, want, buf.String())
	}
}

func TestDebugLevelNeverWarns(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)
	var tr Tracker

	tr.Failed(log, Debug, "optional", errors.New("absent"), "optional absent")
	tr.Failed(log, Debug, "optional", errors.New("absent"), "optional absent")

	for _, l := range levels(&buf) {
		if l != "debug" {
			t.Fatalf("unexpected level %s in %s", l, buf.String())
		}
	}
}

func TestKeysAreIndependent(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)
	var tr Tracker

	tr.Failed(log, Warn, "a", errors.New("x"), "a failed")
	tr.Failed(log, Warn, "b", errors.New("x"), "b failed")

	if got := strings.Join(levels(&buf), ","); got != "warn,warn" {
		t.Fatalf("levels = %s, want warn,warn", got)
	}
}

func TestSuccessWithoutFailureIsSilent(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)
	var tr Tracker

	tr.Succeeded(log, "never-failed", "recovered")

	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %s", buf.String())
	}
}
