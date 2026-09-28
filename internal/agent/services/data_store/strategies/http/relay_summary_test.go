package http

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
)

// A syslog or OTLP receiver probe holds no metric by nature. Its Nagios
// summary answered CRITICAL "no metrics" for ever; it now follows the
// probe's own state.
func TestARelayProbeSummaryFollowsTheProbeState(t *testing.T) {
	agentstate.SetActiveProbes([]string{"id-syslog", "id-otlp"})
	agentstate.SetActiveProbeNames(map[string]string{"Syslog": "id-syslog", "otlp-in": "id-otlp"})
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
		agentstate.SetActiveProbeNames(nil)
	})
	agentstate.RecordProbeHealth("id-syslog", true)
	agentstate.RecordProbeHealth("id-otlp", false)
	agentstate.RecordProbeError("id-otlp", "listener closed")

	if st, body := relayProbeSummary("syslog"); st != 0 || !strings.HasPrefix(body, "OK") {
		t.Errorf("a healthy relay probe: %d %q", st, body)
	}
	if st, body := relayProbeSummary("otlp-in"); st != 2 || !strings.Contains(body, "listener closed") {
		t.Errorf("a failing relay probe: %d %q", st, body)
	}
	if st, _ := relayProbeSummary("nope"); st != 2 {
		t.Errorf("an unknown probe must stay CRITICAL, got %d", st)
	}
}
