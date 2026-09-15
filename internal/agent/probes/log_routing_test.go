package probes_test

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// logProducers are the probes that publish onto the log rail. Their log
// routing has to come from configuration, not from their metric target
// list — see the syslog test below for why that distinction is not
// academic.
var logProducers = []string{
	"syslog", "event", "filetail", "snmp_trap",
	"linux_logs", "windows_eventlog", "otlp_receiver", "kubernetes",
}

func constructProbe(t *testing.T, name string, cfg configuration.ProbeConfig) types.Probe {
	t.Helper()

	ctor, ok := probes.LookupProbeConstructor(name)
	if !ok {
		t.Skipf("probe %q is not registered in this build", name)
	}
	params := map[string]interface{}{"interval": 30}
	for k, v := range probeConfigFixtures[name] {
		params[k] = v
	}
	probe, err := ctor(params, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("constructing %q: %v", name, err)
	}
	if routable, ok := probe.(interface{ SetLogTargets([]string) }); ok {
		routable.SetLogTargets(cfg.LogStrategies)
	}
	return probe
}

// TestLogProducersCarryConfiguredRouting pins that every log producer
// can be routed, and that an unconfigured one broadcasts — which is what
// every existing configuration relies on.
func TestLogProducersCarryConfiguredRouting(t *testing.T) {
	for _, name := range logProducers {
		t.Run(name, func(t *testing.T) {
			probe := constructProbe(t, name, configuration.ProbeConfig{
				LogStrategies: []string{"otlp"},
			})

			router, ok := probe.(interface{ LogTargets() []string })
			if !ok {
				t.Fatalf("%q publishes logs but cannot be routed — it must embed *types.BaseProbe", name)
			}
			got := router.LogTargets()
			if len(got) != 1 || got[0] != "otlp" {
				t.Errorf("%q log targets = %v, want [otlp]", name, got)
			}

			unrouted := constructProbe(t, name, configuration.ProbeConfig{})
			if targets := unrouted.(interface{ LogTargets() []string }).LogTargets(); len(targets) != 0 {
				t.Errorf("%q with no log_strategies returned %v, want none (broadcast)", name, targets)
			}
		})
	}
}

// TestSyslogLogRoutingIsNotItsMetricRouting is the regression this whole
// contract exists to prevent.
//
// The syslog probe sends its METRICS to the legacy event sink, so it
// overrides GetTargetStrategies to ["event"]. Reusing that list for its
// log records — the one-line wiring this looked like — would make
// recordRoutesTo(["event"], "otlp") false and cut syslog logs off the
// OTLP rail entirely. Silent data loss on a rail customers use.
func TestSyslogLogRoutingIsNotItsMetricRouting(t *testing.T) {
	probe := constructProbe(t, "syslog", configuration.ProbeConfig{})

	router, ok := probe.(interface{ GetTargetStrategies() []string })
	if !ok {
		t.Fatal("syslog probe has no metric router")
	}
	metricTargets := router.GetTargetStrategies()
	if len(metricTargets) == 0 {
		t.Skip("syslog no longer overrides its metric routing; the trap this guards is gone")
	}

	logTargets := probe.(interface{ LogTargets() []string }).LogTargets()
	_ = logTargets
	if len(logTargets) != 0 {
		t.Fatalf("unconfigured syslog log routing = %v, want none", logTargets)
	}

	// With no configured routing the record broadcasts, so it reaches
	// the OTLP subscriber. Borrowing the metric list would not.
	if !recordReaches(logTargets, "otlp") {
		t.Error("unconfigured syslog logs do not reach the OTLP rail")
	}
	if recordReaches(metricTargets, "otlp") {
		t.Errorf("metric targets %v happen to include otlp, so this test proves nothing — pick another probe", metricTargets)
	}
}

// recordReaches mirrors the routing decision the log fan-out makes, so
// the test asserts on the real rule rather than on a restatement of it.
func recordReaches(targets []string, subscriber string) bool {
	rec := agentstate.LogRecord{TargetStrategies: targets}
	ch := agentstate.SubscribeLogsFor(subscriber, 4)
	defer agentstate.UnsubscribeLogs(ch)

	agentstate.PublishLog(rec)
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TestConfiguredRoutingReachesOnlyTheNamedOutput is the feature itself:
// an operator who restricts a probe's logs to one output gets exactly
// that, and the other log output stops receiving them.
func TestConfiguredRoutingReachesOnlyTheNamedOutput(t *testing.T) {
	otlp := agentstate.SubscribeLogsFor("otlp", 4)
	defer agentstate.UnsubscribeLogs(otlp)
	event := agentstate.SubscribeLogsFor("event", 4)
	defer agentstate.UnsubscribeLogs(event)

	probe := constructProbe(t, "filetail", configuration.ProbeConfig{
		LogStrategies: []string{"otlp"},
	})
	targets := probe.(interface{ LogTargets() []string }).LogTargets()

	agentstate.PublishLog(agentstate.LogRecord{
		Body:             "routed record",
		TargetStrategies: targets,
	})

	select {
	case <-otlp:
	default:
		t.Error("a record routed to otlp did not reach the otlp subscriber")
	}
	select {
	case rec := <-event:
		t.Errorf("a record routed to otlp ALSO reached the event subscriber: %+v", rec)
	default:
	}
}

// TestProbePollerAppliesConfiguredLogRouting pins the wiring: the
// routing is configuration, so the poller is what applies it. A probe
// never sets its own.
func TestProbePollerAppliesConfiguredLogRouting(t *testing.T) {
	poller, err := probes.NewProbePoller(
		configuration.ProbeConfig{
			Name:          "tail-routed",
			Type:          "filetail",
			Params:        probeConfigFixtures["filetail"],
			LogStrategies: []string{"event"},
		},
		logger.NewLogger(&cliArgs.ParsedArgs{}),
		func([]datapoint.DataPoint, data_store.StrategyRouter) error { return nil },
	)
	if err != nil {
		t.Fatalf("NewProbePoller: %v", err)
	}

	router, ok := poller.Probe.(interface{ LogTargets() []string })
	if !ok {
		t.Fatal("filetail probe cannot be routed")
	}
	got := router.LogTargets()
	if len(got) != 1 || got[0] != "event" {
		t.Errorf("poller did not apply log_strategies: got %v, want [event]", got)
	}
}
