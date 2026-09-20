package zabbix

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func TestBuildKeyQuotesOnlyWhatZabbixNeedsQuoted(t *testing.T) {
	cases := []struct {
		name   string
		params []string
		want   string
	}{
		{"no params", nil, "senhub.system.cpu.utilization"},
		{"plain", []string{"cpu-01", "0"}, "senhub.system.cpu.utilization[cpu-01,0]"},
		{"comma", []string{"db", "a,b"}, `senhub.system.cpu.utilization[db,"a,b"]`},
		{"bracket", []string{"db", "x]"}, `senhub.system.cpu.utilization[db,"x]"]`},
		{"quote", []string{"db", `say "hi"`}, `senhub.system.cpu.utilization[db,"say \"hi\""]`},
		{"leading space", []string{"db", " x"}, `senhub.system.cpu.utilization[db," x"]`},
		{"empty dimension", []string{"db", ""}, "senhub.system.cpu.utilization[db,]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildKey("senhub", "system.cpu.utilization", c.params); got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestBuildKeyDoesNotDoubleTheVendorPrefix(t *testing.T) {
	if got := buildKey("senhub", "senhub.system.paging.limit", []string{"memory"}); got != "senhub.system.paging.limit[memory]" {
		t.Errorf("got %s", got)
	}
	if got := buildKey("acme", "senhub.system.paging.limit", nil); got != "acme.senhub.system.paging.limit" {
		t.Errorf("another prefix still applies: %s", got)
	}
}

func TestSanitizeKeyNameKeepsTheZabbixAlphabet(t *testing.T) {
	if got := sanitizeKeyName("net/if:rx bytes"); got != "net_if_rx_bytes" {
		t.Errorf("got %q", got)
	}
}

func cpuDefinition() *transformers.ProbeDefinition {
	return &transformers.ProbeDefinition{
		ProbeName:           "cpu",
		MultiInstanceLabels: []string{"cpu"},
		Metrics: []transformers.MetricDefinition{
			{
				Name: "cpu.usage", Unit: "%", DisplayName: "CPU {cpu} usage",
				Otel: &transformers.OtelMapping{Name: "system.cpu.utilization", Unit: "1", Type: "gauge", Attributes: map[string]string{"cpu.mode": "user"}},
			},
			{
				Name: "cpu.system", Unit: "%",
				Otel: &transformers.OtelMapping{Name: "system.cpu.utilization", Unit: "1", Type: "gauge", Attributes: map[string]string{"cpu.mode": "system", "cpu.kind": "kernel"}},
			},
			{
				Name: "cpu.state", Unit: "#", Lookup: "senhub.cpu.state",
				Otel: &transformers.OtelMapping{
					Name: "senhub.cpu.state", Unit: "1", Type: "gauge",
					Expand: &transformers.ExpandDirective{Attribute: "cpu.state", Mapping: map[string]int{"ok": 0, "hot": 1}},
				},
			},
			{Name: "cpu.raw", Unit: "#"},
		},
	}
}

func TestItemForUsesTheOtelNameAndUnitConvertedValue(t *testing.T) {
	def := cpuDefinition()
	cm := otelmapper.CacheMetric{
		ProbeName: "host-cpu", ProbeType: "cpu", MetricName: "cpu.usage", Value: 42.5, Unit: "%",
		Tags: map[string]string{"cpu": "3", "probe_name": "host-cpu", "probe_type": "cpu"},
	}
	it := itemFor("senhub", def, cm)
	if it.Key != "senhub.system.cpu.utilization[host-cpu,3,user]" {
		t.Errorf("key = %s", it.Key)
	}
	if it.Value != "0.425" || it.Unit != "1" {
		t.Errorf("value = %s %s, want 0.425 1 (percent becomes a ratio)", it.Value, it.Unit)
	}
}

func TestItemForTellsApartMetricsCollapsedOnOneOtelName(t *testing.T) {
	def := cpuDefinition()
	tagsOf := func(cpu string) map[string]string {
		return map[string]string{"cpu": cpu, "probe_name": "host-cpu", "probe_type": "cpu"}
	}
	user := itemFor("senhub", def, otelmapper.CacheMetric{ProbeName: "host-cpu", ProbeType: "cpu", MetricName: "cpu.usage", Value: 10, Unit: "%", Tags: tagsOf("0")})
	system := itemFor("senhub", def, otelmapper.CacheMetric{ProbeName: "host-cpu", ProbeType: "cpu", MetricName: "cpu.system", Value: 5, Unit: "%", Tags: tagsOf("0")})
	if user.Key == system.Key {
		t.Fatalf("two internal metrics on one OTel name got the same key %s", user.Key)
	}
	if system.Key != "senhub.system.cpu.utilization[host-cpu,0,kernel,system]" {
		t.Errorf("static attributes must follow in key order: %s", system.Key)
	}
}

func TestItemForSendsAnEnumAsItsRawCodeUnderOneKey(t *testing.T) {
	def := cpuDefinition()
	cm := otelmapper.CacheMetric{
		ProbeName: "host-cpu", ProbeType: "cpu", MetricName: "cpu.state", Value: 1, Unit: "#",
		Tags: map[string]string{"cpu": "0", "probe_name": "host-cpu", "probe_type": "cpu"},
	}
	it := itemFor("senhub", def, cm)
	if it.Key != "senhub.cpu.state[host-cpu,0]" || it.Value != "1" {
		t.Errorf("item = %+v", it)
	}
}

func TestItemForFallsBackToTheInternalName(t *testing.T) {
	def := cpuDefinition()
	cm := otelmapper.CacheMetric{
		ProbeName: "host-cpu", ProbeType: "cpu", MetricName: "cpu.raw", Value: 7, Unit: "#",
		Tags: map[string]string{"cpu": "1"},
	}
	if it := itemFor("senhub", def, cm); it.Key != "senhub.cpu.raw[host-cpu,1]" || it.Value != "7" {
		t.Errorf("item = %+v", it)
	}
	noDef := otelmapper.CacheMetric{ProbeName: "p", ProbeType: "unknown", MetricName: "x.y", Value: 3}
	if it := itemFor("senhub", nil, noDef); it.Key != "senhub.x.y[p]" || it.Value != "3" {
		t.Errorf("item = %+v", it)
	}
}
