package probes_test

import (
	"time"

	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/cpu"
	"senhub-agent.go/internal/agent/probes/logicaldisk"
	"senhub-agent.go/internal/agent/probes/memory"
	"senhub-agent.go/internal/agent/probes/network"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestHostProbeOutput_EveryMetricHasDefinition runs the four host
// probes' real Collect() on the test machine and asserts every emitted
// metric name has an entry in the probe's transformer definition. A
// missing entry means the metric silently falls through to the default
// name/unit on every sink — the drift class #316 item 3 guards against
// (probes changed names across many merges with no semantic
// re-validation).
func TestHostProbeOutput_EveryMetricHasDefinition(t *testing.T) {
	defs, err := transformers.DefinitionMetricNames()
	if err != nil {
		t.Fatalf("DefinitionMetricNames: %v", err)
	}

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	config := map[string]interface{}{"interval": 60}

	probes := map[string]func(map[string]interface{}, *logger.Logger) (types.Probe, error){
		"cpu":         cpu.NewCpuProbe,
		"memory":      memory.NewMemoryProbe,
		"network":     network.NewNetworkProbe,
		"logicaldisk": logicaldisk.NewLogicalDiskProbe,
	}

	for probeName, constructor := range probes {
		t.Run(probeName, func(t *testing.T) {
			defined := make(map[string]bool)
			for _, n := range defs[probeName] {
				defined[n] = true
			}
			if len(defined) == 0 {
				t.Fatalf("no definition metrics for probe %s", probeName)
			}

			probe, err := constructor(config, baseLogger)
			if err != nil {
				// A constructor failure is an environment limitation
				// (e.g. PDH_NO_DATA on headless Windows CI runners
				// with no network counters), not a name drift — there
				// is no collector to validate. Real-hardware coverage
				// comes from runtime validation on bbcloud.
				t.Skipf("constructing %s probe not possible in this environment: %v", probeName, err)
			}
			points, err := probe.Collect()
			if err != nil {
				t.Fatalf("%s Collect: %v", probeName, err)
			}
			if len(points) == 0 {
				// Rate-computing probes (network) emit nothing on the
				// first call: it only establishes the counter baseline.
				time.Sleep(200 * time.Millisecond)
				points, err = probe.Collect()
				if err != nil {
					t.Fatalf("%s second Collect: %v", probeName, err)
				}
			}
			if len(points) == 0 {
				t.Fatalf("%s Collect returned no datapoints", probeName)
			}

			missing := map[string]bool{}
			for _, dp := range points {
				if !defined[dp.Name] {
					missing[dp.Name] = true
				}
			}
			for name := range missing {
				t.Errorf("%s emits %q with no transformer definition entry — it falls through to default name/unit on every sink", probeName, name)
			}
		})
	}
}
