package transformers

import (
	"fmt"
	"sync"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestTransformerRegistry_ConcurrentAccess reproduces the #259 access
// pattern under the race detector: probe scheduler goroutines loading
// transformers (cache writes on first load), HTTP scrape handlers
// resolving probe definitions, and OTLP-style per-series definition
// lookups — all concurrently. The historical registry had no
// synchronization: this pattern was a `fatal: concurrent map writes`
// waiting for the startup first-load window.
func TestTransformerRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewTransformerRegistry(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))

	probes := []string{"cpu", "memory", "network", "logicaldisk", "snmp_poll", "mysql", "postgresql", "redfish"}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		// Writers: first-load transformers (probe schedulers at startup).
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				probe := probes[(n+j)%len(probes)]
				if _, err := registry.LoadTransformer(probe, "friendly"); err != nil {
					t.Errorf("LoadTransformer(%s): %v", probe, err)
					return
				}
				// Unknown probe types exercise the fallback path.
				if _, err := registry.LoadTransformer(fmt.Sprintf("custom_%d", n), "friendly"); err != nil {
					t.Errorf("LoadTransformer(custom): %v", err)
					return
				}
			}
		}(i)

		// Readers: definition lookups (Prometheus scrape + OTLP export),
		// including memoized negatives.
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				probe := probes[(n+j)%len(probes)]
				if def := registry.GetProbeDefinition(probe); def == nil {
					t.Errorf("GetProbeDefinition(%s) returned nil for an embedded probe", probe)
					return
				}
				_ = registry.GetProbeDefinition("does_not_exist")
			}
		}(i)
	}
	wg.Wait()
}

// TestTransformerRegistry_LazyDefinitions pins the footprint contract:
// nothing is parsed at construction, a definition is parsed by the first
// lookup of its probe type only, and every later lookup is an index hit,
// so the OTLP export path never re-parses YAML per series.
func TestTransformerRegistry_LazyDefinitions(t *testing.T) {
	registry := NewTransformerRegistry(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))

	if n := len(registry.read().definitions); n != 0 {
		t.Fatalf("registry parsed %d definitions at construction, want 0", n)
	}
	first := registry.GetProbeDefinition("cpu")
	second := registry.GetProbeDefinition("cpu")
	if first == nil || second == nil {
		t.Fatal("embedded cpu definition not found")
	}
	if first != second {
		t.Error("GetProbeDefinition returned distinct instances; definition is re-parsed instead of served from the index")
	}
	if n := len(registry.read().definitions); n != 1 {
		t.Errorf("looking up one probe type left %d definitions parsed, want 1", n)
	}

	// Negative lookups are memoized too.
	if registry.GetProbeDefinition("nope") != nil {
		t.Error("unknown probe returned a definition")
	}
	_, memoized := registry.read().definitions["nope"]
	if !memoized {
		t.Error("negative lookup was not memoized")
	}
}

// TestTransformerRegistry_LazyServesEveryEmbeddedProbe guards the move
// from eager to lazy loading: every shipped definition must still be
// reachable by its probe name, through both the definition lookup and the
// transformer built from it.
func TestTransformerRegistry_LazyServesEveryEmbeddedProbe(t *testing.T) {
	registry := NewTransformerRegistry(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	defs, err := Definitions()
	if err != nil {
		t.Fatalf("Definitions(): %v", err)
	}
	for name, want := range defs {
		got := registry.GetProbeDefinition(name)
		if got == nil {
			t.Errorf("GetProbeDefinition(%s) = nil for an embedded probe", name)
			continue
		}
		if got.ProbeName != want.ProbeName || len(got.Metrics) != len(want.Metrics) {
			t.Errorf("%s: lazily loaded definition differs from the embedded one", name)
		}
		if _, ok := mustTransformer(t, registry, name).(*DefinitionBasedTransformer); !ok {
			t.Errorf("%s: LoadTransformer did not build a definition-based transformer", name)
		}
	}
}

func mustTransformer(t *testing.T, registry *TransformerRegistry, name string) MetricTransformer {
	t.Helper()
	tr, err := registry.LoadTransformer(name, "friendly")
	if err != nil {
		t.Fatalf("LoadTransformer(%s): %v", name, err)
	}
	return tr
}
