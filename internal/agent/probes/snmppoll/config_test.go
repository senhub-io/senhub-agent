package snmppoll

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"target": "192.0.2.10",
		"mibs":   []interface{}{"if-mib"},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Port != defaultPort || cfg.Community != defaultCommunity {
		t.Errorf("defaults not applied: port=%d community=%q", cfg.Port, cfg.Community)
	}
	if cfg.Timeout != defaultTimeout || cfg.Retries != defaultRetries || cfg.Interval != defaultInterval {
		t.Errorf("default timeout/retries/interval not applied: %+v", cfg)
	}
	if len(cfg.MIBs) != 1 || cfg.MIBs[0] != "if-mib" {
		t.Errorf("unexpected MIBs: %+v", cfg.MIBs)
	}
}

func TestParseConfig_Overrides(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"target":    "192.0.2.10",
		"port":      1161,
		"community": "secret",
		"version":   "v2c",
		"timeout":   "10s",
		"retries":   4,
		"interval":  30,
		"mibs":      []interface{}{"mib-2", "if-mib"},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Port != 1161 || cfg.Community != "secret" {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", cfg.Timeout)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", cfg.Interval)
	}
	if cfg.Retries != 4 {
		t.Errorf("retries = %d, want 4", cfg.Retries)
	}
}

func TestParseConfig_CustomMappings(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"target": "192.0.2.10",
		"custom_mappings": []interface{}{
			map[string]interface{}{"oid": ".1.3.6.1.4.1.9.1.1", "metric": "senhub.snmp.vendorTemp", "type": "gauge"},
			map[interface{}]interface{}{"oid": "1.3.6.1.4.1.9.2.1", "metric": "senhub.snmp.fanRpm", "type": "counter", "index_label": "fan_index"},
		},
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(cfg.Custom) != 2 {
		t.Fatalf("expected 2 custom mappings, got %d", len(cfg.Custom))
	}
	// Leading dot trimmed, kinds parsed, yaml.v2 nested map tolerated.
	if cfg.Custom[0].OID != "1.3.6.1.4.1.9.1.1" || cfg.Custom[0].Kind != kindGauge {
		t.Errorf("custom[0] wrong: %+v", cfg.Custom[0])
	}
	if cfg.Custom[1].Kind != kindCounter || cfg.Custom[1].IndexLabel != "fan_index" {
		t.Errorf("custom[1] wrong: %+v", cfg.Custom[1])
	}
}

func TestParseConfig_Errors(t *testing.T) {
	cases := []struct {
		name    string
		raw     map[string]interface{}
		wantSub string
	}{
		{"missing target", map[string]interface{}{"mibs": []interface{}{"if-mib"}}, "target is required"},
		{"no mibs or custom", map[string]interface{}{"target": "192.0.2.10"}, "at least one entry"},
		{"bad port", map[string]interface{}{"target": "192.0.2.10", "port": 99999, "mibs": []interface{}{"if-mib"}}, "port must be between"},
		{"snmpv1 rejected", map[string]interface{}{"target": "192.0.2.10", "version": "v1", "mibs": []interface{}{"if-mib"}}, "SNMPv1 is not supported"},
		{"snmpv3 rejected", map[string]interface{}{"target": "192.0.2.10", "version": "v3", "mibs": []interface{}{"if-mib"}}, "SNMPv3 is not supported"},
		{"unknown mib", map[string]interface{}{"target": "192.0.2.10", "mibs": []interface{}{"made-up"}}, "unknown built-in MIB"},
		{"custom missing oid", map[string]interface{}{"target": "192.0.2.10", "custom_mappings": []interface{}{map[string]interface{}{"metric": "x"}}}, "requires 'oid'"},
		{"custom missing metric", map[string]interface{}{"target": "192.0.2.10", "custom_mappings": []interface{}{map[string]interface{}{"oid": ".1.2.3"}}}, "requires 'metric'"},
		{"bad timeout string", map[string]interface{}{"target": "192.0.2.10", "mibs": []interface{}{"if-mib"}, "timeout": "soon"}, "timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseConfig(c.raw)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), c.wantSub)
			}
		})
	}
}
