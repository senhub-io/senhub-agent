package template

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func platformDefinition() transformers.ProbeDefinition {
	return transformers.ProbeDefinition{
		ProbeName:    "cpu",
		FriendlyName: "CPU",
		Metrics: []transformers.MetricDefinition{
			{
				Name: "cpu_idle", DisplayName: "CPU Idle",
				Otel: &transformers.OtelMapping{Name: "system.cpu.utilization", Unit: "1", Type: "gauge"},
			},
			{
				Name: "cpu_dpc_rate", DisplayName: "CPU DPCs", Platforms: []string{"windows"},
				Otel: &transformers.OtelMapping{Name: "senhub.system.cpu.dpcs", Unit: "1/s", Type: "gauge"},
			},
		},
	}
}

func TestAPlatformOnlyMetricIsNotDeclaredWhereItCannotBeFed(t *testing.T) {
	linux, err := Generate(platformDefinition(), Options{Prefix: "senhub", Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if keysOfTemplate(linux)["senhub.system.cpu.dpcs[{#PROBE}]"] {
		t.Error("a Windows counter is declared on Linux, where it can only ever be an empty item")
	}
	if !keysOfTemplate(linux)["senhub.system.cpu.utilization[{#PROBE}]"] {
		t.Error("the portable metric was dropped with it")
	}

	windows, err := Generate(platformDefinition(), Options{Prefix: "senhub", Platform: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if !keysOfTemplate(windows)["senhub.system.cpu.dpcs[{#PROBE}]"] {
		t.Error("the Windows counter is missing from the Windows template")
	}
}

func TestWithoutAPlatformEveryMetricIsStillDeclared(t *testing.T) {
	exp, err := Generate(platformDefinition(), Options{Prefix: "senhub"})
	if err != nil {
		t.Fatal(err)
	}
	if !keysOfTemplate(exp)["senhub.system.cpu.dpcs[{#PROBE}]"] {
		t.Error("asking for no platform must keep the definition whole")
	}
	if n := exp.ZabbixExport.Templates[0].Template; strings.Contains(n, "(") {
		t.Errorf("template = %q; only a platform-specific one is named for its platform", n)
	}
}

func keysOfTemplate(e Export) map[string]bool {
	out := map[string]bool{}
	for _, r := range e.ZabbixExport.Templates[0].DiscoveryRules {
		for _, p := range r.ItemPrototypes {
			out[p.Key] = true
		}
	}
	return out
}

func TestRunsOnTreatsAnEmptyListAsEverywhere(t *testing.T) {
	anywhere := transformers.MetricDefinition{}
	for _, goos := range []string{"linux", "windows", "darwin"} {
		if !anywhere.RunsOn(goos) {
			t.Errorf("a metric naming no platform must run on %s", goos)
		}
	}
	windowsOnly := transformers.MetricDefinition{Platforms: []string{"windows"}}
	if windowsOnly.RunsOn("linux") || !windowsOnly.RunsOn("windows") {
		t.Error("the platform list is not honoured")
	}
}
