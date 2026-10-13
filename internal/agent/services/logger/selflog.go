package logger

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/agentstate"
)

const (
	selfLogRatePerSec = 20.0
	selfLogBurst      = 100.0
)

var selfLogStats = struct {
	published atomic.Uint64
	dropped   atomic.Uint64
}{}

type selfLogWriter struct {
	minLevel zerolog.Level
	publish  func(agentstate.LogRecord)
	now      func() time.Time

	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
}

func selfLogLevelFromEnv() zerolog.Level {
	switch os.Getenv("SENHUB_SELF_LOGS") {
	case "error":
		return zerolog.ErrorLevel
	case "off":
		return zerolog.Disabled
	default:
		return zerolog.WarnLevel
	}
}

func newSelfLogWriter(minLevel zerolog.Level) *selfLogWriter {
	if minLevel == zerolog.Disabled {
		return nil
	}
	return newSelfLogWriterWithClock(minLevel, time.Now)
}

func newSelfLogWriterWithClock(minLevel zerolog.Level, now func() time.Time) *selfLogWriter {
	w := &selfLogWriter{
		minLevel: minLevel,
		publish:  agentstate.PublishLog,
		now:      now,
		tokens:   selfLogBurst,
	}
	w.lastRefill = now()
	return w
}

// SelfLogsPublished returns the lifetime number of self-log records handed
// to the OTLP log rail.
func SelfLogsPublished() uint64 {
	return selfLogStats.published.Load()
}

// SelfLogsDropped returns the lifetime number of self-log records dropped by
// the rate limiter.
func SelfLogsDropped() uint64 {
	return selfLogStats.dropped.Load()
}

func (w *selfLogWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	if w == nil || w.minLevel == zerolog.Disabled || level < w.minLevel {
		return len(p), nil
	}

	// Masking happens here, after the level check, rather than through a
	// MaskingWriter in front: that one parses every event, debug included.
	var evt map[string]any
	if err := json.Unmarshal(p, &evt); err != nil {
		return len(p), nil
	}
	evt = maskJSONFields(evt)

	if module, _ := evt["module"].(string); w.pipelineLoopModule(module) {
		return len(p), nil
	}

	if !w.allow() {
		selfLogStats.dropped.Add(1)
		return len(p), nil
	}

	rec := buildSelfLogRecord(evt, level, w.now())
	if w.publish != nil {
		w.publish(rec)
	}
	selfLogStats.published.Add(1)
	return len(p), nil
}

func (w *selfLogWriter) Write(p []byte) (int, error) {
	var envelope struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal(p, &envelope); err != nil {
		return len(p), nil
	}

	var level zerolog.Level
	switch envelope.Level {
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	case "fatal":
		level = zerolog.FatalLevel
	case "panic":
		level = zerolog.PanicLevel
	default:
		return len(p), nil
	}
	return w.WriteLevel(level, p)
}

func (w *selfLogWriter) pipelineLoopModule(module string) bool {
	return strings.HasPrefix(module, "otlp") ||
		strings.HasPrefix(module, "strategy.otlp") ||
		strings.HasPrefix(module, "logs")
}

func (w *selfLogWriter) allow() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := w.now()
	if elapsed := now.Sub(w.lastRefill).Seconds(); elapsed > 0 {
		w.tokens += elapsed * selfLogRatePerSec
		if w.tokens > selfLogBurst {
			w.tokens = selfLogBurst
		}
		w.lastRefill = now
	}
	if w.tokens < 1 {
		return false
	}
	w.tokens--
	return true
}

func buildSelfLogRecord(evt map[string]any, level zerolog.Level, fallbackTime time.Time) agentstate.LogRecord {
	msg, _ := evt["message"].(string)
	if errVal, ok := evt["error"]; ok {
		errText := ""
		switch e := errVal.(type) {
		case string:
			errText = e
		case nil:
			// No error text to append.
		default:
			if b, err := json.Marshal(e); err == nil {
				errText = string(b)
			}
		}
		if errText != "" {
			msg += " error=" + errText
		}
	}

	ts := fallbackTime
	if s, ok := evt["time"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			ts = parsed
		}
	}

	attrs := make(map[string]string)
	for k, v := range evt {
		switch k {
		case "level", "time", "message", "error", "module":
			continue
		}
		if s, ok := v.(string); ok {
			attrs[k] = s
		}
	}
	if module, _ := evt["module"].(string); module != "" {
		attrs["senhub.agent.module"] = module
	}

	sev, sevText := selfLogSeverity(level)
	return agentstate.LogRecord{
		Timestamp:         ts,
		Severity:          sev,
		SeverityText:      sevText,
		Body:              msg,
		Attributes:        attrs,
		ProducerProbeName: "senhub-agent",
		ProducerProbeType: "agent",
	}
}

func selfLogSeverity(level zerolog.Level) (agentstate.LogSeverity, string) {
	switch level {
	case zerolog.WarnLevel:
		return agentstate.LogSeverityWarn, "WARN"
	case zerolog.ErrorLevel:
		return agentstate.LogSeverityError, "ERROR"
	case zerolog.FatalLevel, zerolog.PanicLevel:
		return agentstate.LogSeverityFatal, "FATAL"
	default:
		return agentstate.LogSeverityWarn, "WARN"
	}
}
