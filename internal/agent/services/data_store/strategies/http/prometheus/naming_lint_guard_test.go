package prometheus

import (
	"sort"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/agentmetrics"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// promlintUnits mirrors the unit table of promtool's metric lint
// (client_golang testutil/promlint): a name token found here, alone or
// after an SI/IEC prefix, must be the base unit.
var promlintUnits = map[string]string{
	"amperes": "amperes", "bytes": "bytes", "celsius": "celsius",
	"grams": "grams", "joules": "joules", "kelvin": "kelvin",
	"meters": "meters", "metres": "metres", "seconds": "seconds",
	"volts":   "volts",
	"minutes": "seconds", "hours": "seconds", "days": "seconds",
	"weeks": "seconds", "bits": "bytes", "fahrenheit": "celsius",
	"farenheit": "celsius", "calories": "joules",
}

var promlintUnitPrefixes = []string{
	"pico", "nano", "micro", "milli", "centi", "deci", "deca", "hecto",
	"kilo", "kibi", "mega", "mibi", "giga", "gibi", "tera", "tebi", "peta", "pebi",
}

var promlintAbbreviations = map[string]bool{
	"s": true, "ms": true, "us": true, "ns": true, "sec": true, "b": true,
	"kb": true, "mb": true, "gb": true, "tb": true, "pb": true, "m": true,
	"h": true, "d": true,
}

// promlintExemptAbbreviation lists names where promtool reads a token as
// an abbreviated unit but the token is a vendor word: `ns` is NetScaler's
// own name for the appliance-wide scope, not nanoseconds.
func promlintExemptAbbreviation(name, token string) bool {
	return token == "ns" && strings.HasPrefix(name, "senhub_netscaler_ns_")
}

// promNameProblems applies promtool's naming lint to one exposed name.
// otelName is used for the one accepted deviation: an OpenTelemetry
// `.count` gauge keeps its `_count` suffix (#978).
func promNameProblems(promName, promType, otelName string) []string {
	var problems []string
	tokens := strings.Split(promName, "_")

	for _, tok := range tokens {
		base, unit := promlintUnits[tok], tok
		if base == "" {
			for _, p := range promlintUnitPrefixes {
				if strings.HasPrefix(tok, p) && promlintUnits[tok[len(p):]] != "" {
					base = promlintUnits[tok[len(p):]]
					break
				}
			}
		}
		if base != "" && base != unit {
			problems = append(problems, "unit "+unit+" is not the base unit "+base)
		}
		if promlintAbbreviations[tok] && !promlintExemptAbbreviation(promName, tok) {
			problems = append(problems, "abbreviated unit "+tok)
		}
	}

	if promType != "counter" && strings.HasSuffix(promName, "_total") {
		problems = append(problems, "a "+promType+" ends in _total, which is reserved for counters")
	}
	if promType == "counter" && !strings.HasSuffix(promName, "_total") {
		problems = append(problems, "a counter does not end in _total")
	}
	if promType != "histogram" && promType != "summary" {
		for _, reserved := range []string{"_sum", "_bucket", "_count"} {
			if !strings.HasSuffix(promName, reserved) {
				continue
			}
			if reserved == "_count" && strings.HasSuffix(otelName, ".count") {
				continue
			}
			problems = append(problems, "a "+promType+" ends in "+reserved+", which is reserved for histograms and summaries")
		}
	}

	// A rate unit already spelled in the OTel name comes out twice once
	// the unit suffix is appended (redis_ops_per_sec_per_second).
	if i := strings.Index(promName, "_per_"); i >= 0 && strings.Contains(promName[i+len("_per_"):], "_per_") {
		problems = append(problems, "the rate unit appears twice")
	}

	if strings.ToLower(promName) != promName {
		problems = append(problems, "not snake_case")
	}
	return problems
}

// TestExposedNamesPassPromlint derives the Prometheus name of every
// metric the agent exposes, probe definitions and the agent's own
// metrics alike, and applies promtool's naming lint to it.
//
// The relay definition (otlp_receiver) is exempt: it describes names
// other programs send, which the agent passes through untouched.
func TestExposedNamesPassPromlint(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	probes := make([]string, 0, len(defs))
	for p := range defs {
		probes = append(probes, p)
	}
	sort.Strings(probes)
	for _, probe := range probes {
		if probe == "otlp_receiver" {
			continue
		}
		for _, m := range defs[probe].Metrics {
			if m.Otel == nil || m.Otel.Skip || m.Otel.Name == "" {
				continue
			}
			promName := OTelNameToPromName(m.Otel.Name, m.Otel.Unit, m.Otel.Type)
			for _, p := range promNameProblems(promName, PromType(m.Otel.Type), m.Otel.Name) {
				t.Errorf("%s/%s: %s (OTel %q, unit %q): %s", probe, m.Name, promName, m.Otel.Name, m.Otel.Unit, p)
			}
		}
	}

	for _, r := range agentmetrics.BuildAgentRecords(agentmetrics.AgentMetricsSnapshot{StartTime: time.Now()}) {
		promName := OTelNameToPromName(r.Name, r.Unit, r.Type)
		for _, p := range promNameProblems(promName, PromType(r.Type), r.Name) {
			t.Errorf("agent self-metric %s (OTel %q): %s", promName, r.Name, p)
		}
	}
}

func TestPromNameProblemsCatchesEachClass(t *testing.T) {
	cases := []struct {
		name, promType, otelName string
	}{
		{"senhub_agent_probes_total", "gauge", "senhub.agent.probes.total"},
		{"senhub_kafka_consumer_group_lag_sum", "gauge", "kafka.consumer_group.lag_sum"},
		{"senhub_redis_ops_per_sec_per_second", "gauge", "redis.ops.per_sec"},
		{"senhub_netscaler_ssl_certificate_days_to_expiration", "gauge", "senhub.netscaler.ssl.certificate.days_to_expiration"},
		{"senhub_x_latency_milliseconds", "gauge", "x.latency"},
		{"senhub_x_speed_bits_per_second", "gauge", "x.speed"},
		{"senhub_x_items_bucket", "gauge", "x.items"},
		{"senhub_x_items_count", "gauge", "x.items_count"},
		{"senhub_x_requests", "counter", "x.requests"},
	}
	for _, c := range cases {
		if len(promNameProblems(c.name, c.promType, c.otelName)) == 0 {
			t.Errorf("%s (%s) passed the lint, want a problem", c.name, c.promType)
		}
	}

	for _, ok := range []struct{ name, promType, otelName string }{
		{"senhub_system_process_count", "gauge", "system.process.count"},
		{"senhub_netscaler_ns_throughput_bytes_per_second", "gauge", "senhub.netscaler.ns.throughput"},
		{"senhub_system_cpu_time_seconds_total", "counter", "system.cpu.time"},
	} {
		if p := promNameProblems(ok.name, ok.promType, ok.otelName); len(p) != 0 {
			t.Errorf("%s flagged: %v", ok.name, p)
		}
	}
}
