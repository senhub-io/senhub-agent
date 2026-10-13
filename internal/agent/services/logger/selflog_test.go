package logger

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
)

type recordSink struct {
	mu   sync.Mutex
	recs []agentstate.LogRecord
}

func (s *recordSink) publish(r agentstate.LogRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, r)
}

func (s *recordSink) all() []agentstate.LogRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agentstate.LogRecord(nil), s.recs...)
}

func newTestSelfLog(t *testing.T, min zerolog.Level) (*selfLogWriter, *recordSink, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	w := newSelfLogWriterWithClock(min, func() time.Time { return now })
	sink := &recordSink{}
	w.publish = sink.publish
	return w, sink, &now
}

func emit(t *testing.T, w *selfLogWriter, fn func(zerolog.Logger)) {
	t.Helper()
	var console bytes.Buffer
	fn(zerolog.New(zerolog.MultiLevelWriter(&console, w)).With().Timestamp().Logger())
}

func TestSelfLogPublishesWarnWithModuleAndMaskedSecret(t *testing.T) {
	w, sink, _ := newTestSelfLog(t, zerolog.WarnLevel)
	emit(t, w, func(l zerolog.Logger) {
		l.Warn().Str("module", "probe.azure_container_apps").Str("password", "hunter2").Msg("permission missing")
		l.Info().Str("module", "probe.cpu").Msg("collected")
	})

	recs := sink.all()
	if len(recs) != 1 {
		t.Fatalf("published %d records, want 1 (info must be ignored)", len(recs))
	}
	r := recs[0]
	if r.Severity != agentstate.LogSeverityWarn || r.SeverityText != "WARN" {
		t.Errorf("severity = %v %q, want WARN", r.Severity, r.SeverityText)
	}
	if r.Body != "permission missing" {
		t.Errorf("body = %q", r.Body)
	}
	if r.Attributes["senhub.agent.module"] != "probe.azure_container_apps" {
		t.Errorf("module attribute = %q", r.Attributes["senhub.agent.module"])
	}
	if got := r.Attributes["password"]; got == "hunter2" {
		t.Errorf("password attribute leaked unmasked")
	}
	if r.ProducerProbeType != "agent" {
		t.Errorf("producer type = %q, want agent", r.ProducerProbeType)
	}
}

func TestSelfLogErrorAppendsTheError(t *testing.T) {
	w, sink, _ := newTestSelfLog(t, zerolog.WarnLevel)
	emit(t, w, func(l zerolog.Logger) {
		l.Error().Str("module", "probe.redis").Err(errBoom{}).Msg("connect failed")
	})
	recs := sink.all()
	if len(recs) != 1 || recs[0].Body != "connect failed error=boom" || recs[0].SeverityText != "ERROR" {
		t.Fatalf("records = %+v", recs)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func TestSelfLogErrorLevelIgnoresWarn(t *testing.T) {
	w, sink, _ := newTestSelfLog(t, zerolog.ErrorLevel)
	emit(t, w, func(l zerolog.Logger) {
		l.Warn().Str("module", "probe.x").Msg("w")
		l.Error().Str("module", "probe.x").Msg("e")
	})
	if recs := sink.all(); len(recs) != 1 || recs[0].Body != "e" {
		t.Fatalf("records = %+v, want only the error", recs)
	}
}

func TestSelfLogOffBuildsNoWriter(t *testing.T) {
	t.Setenv("SENHUB_SELF_LOGS", "off")
	if w := newSelfLogWriter(selfLogLevelFromEnv()); w != nil {
		t.Fatal("SENHUB_SELF_LOGS=off must not build a writer")
	}
}

func TestSelfLogSkipsTheLogPipelineItself(t *testing.T) {
	w, sink, _ := newTestSelfLog(t, zerolog.WarnLevel)
	emit(t, w, func(l zerolog.Logger) {
		for _, m := range []string{"otlp", "strategy.otlp.logs", "logs.shipper"} {
			l.Warn().Str("module", m).Msg("export failed")
		}
	})
	if recs := sink.all(); len(recs) != 0 {
		t.Fatalf("pipeline modules republished: %+v", recs)
	}
}

func TestSelfLogRateLimit(t *testing.T) {
	w, sink, now := newTestSelfLog(t, zerolog.WarnLevel)
	droppedBefore := SelfLogsDropped()
	emit(t, w, func(l zerolog.Logger) {
		for i := 0; i < 150; i++ {
			l.Warn().Str("module", "probe.x").Msg("flood")
		}
	})
	if got := len(sink.all()); got != int(selfLogBurst) {
		t.Fatalf("published %d in a burst, want %d", got, int(selfLogBurst))
	}
	if d := SelfLogsDropped() - droppedBefore; d != 50 {
		t.Fatalf("dropped %d, want 50", d)
	}

	// One second later the bucket holds 20 tokens again, not more: the
	// refill must not be counted twice across denied calls.
	*now = now.Add(time.Second)
	emit(t, w, func(l zerolog.Logger) {
		for i := 0; i < 30; i++ {
			l.Warn().Str("module", "probe.x").Msg("flood")
		}
	})
	if got := len(sink.all()) - int(selfLogBurst); got != int(selfLogRatePerSec) {
		t.Fatalf("published %d after 1 s, want %d", got, int(selfLogRatePerSec))
	}
}

func TestSelfLogConcurrentWrites(t *testing.T) {
	w, sink, _ := newTestSelfLog(t, zerolog.WarnLevel)
	l := zerolog.New(w)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				l.Warn().Str("module", "probe.x").Msg("m")
			}
		}()
	}
	wg.Wait()
	if got := len(sink.all()); got != 80 {
		t.Fatalf("published %d, want 80", got)
	}
}
