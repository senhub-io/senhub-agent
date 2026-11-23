package otlp

import (
	"testing"
)

func TestQueueSwitches_AreIndependent(t *testing.T) {
	clearQueueEnv(t)
	t.Setenv("SENHUB_LOG_QUEUE", "false")
	p := parseWith(t, nil).Persistence
	if p.logsQueuePath() != "" || p.metricsQueuePath() == "" || p.tracesQueuePath() == "" {
		t.Errorf("SENHUB_LOG_QUEUE=false must switch off logs only: %q %q %q", p.logsQueuePath(), p.metricsQueuePath(), p.tracesQueuePath())
	}
	t.Setenv("SENHUB_LOG_QUEUE", "")
	t.Setenv("SENHUB_TRACE_QUEUE", "false")
	p = parseWith(t, nil).Persistence
	if p.logsQueuePath() == "" || p.metricsQueuePath() == "" || p.tracesQueuePath() != "" {
		t.Errorf("SENHUB_TRACE_QUEUE=false must switch off traces only")
	}
}
