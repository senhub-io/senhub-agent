package snmppoll

import (
	"github.com/gosnmp/gosnmp"

	"net"
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
		{"snmpv3 without credentials block", map[string]interface{}{"target": "192.0.2.10", "version": "v3", "mibs": []interface{}{"if-mib"}}, "requires a 'v3' block"},
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

func TestParseConfig_Discovery(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"target": "10.0.0.1",
		"mibs":   []interface{}{"if-mib"},
		"discovery": map[string]interface{}{
			"seeds":         []interface{}{"10.0.0.1", "10.0.0.2"},
			"profile":       map[string]interface{}{"version": "v2c", "community": "discover-ro"},
			"allowed_cidrs": []interface{}{"10.0.0.0/8", "192.168.0.0/16"},
			"max_hops":      6, // override; max_devices defaults
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	d := cfg.Discovery
	if d == nil {
		t.Fatal("discovery is nil")
	}
	if len(d.Seeds) != 2 || d.Seeds[0] != "10.0.0.1" {
		t.Errorf("seeds = %+v", d.Seeds)
	}
	if d.Profile.Version != "v2c" || d.Profile.Community != "discover-ro" {
		t.Errorf("profile = %+v", d.Profile)
	}
	if len(d.AllowedCIDRs) != 2 {
		t.Fatalf("allowed_cidrs = %d, want 2", len(d.AllowedCIDRs))
	}
	if !d.AllowedCIDRs[0].Contains(net.ParseIP("10.255.1.1")) {
		t.Error("10.0.0.0/8 should contain 10.255.1.1")
	}
	if d.MaxHops != 6 {
		t.Errorf("max_hops = %d, want 6", d.MaxHops)
	}
	if d.MaxDevices != defaultMaxDevices {
		t.Errorf("max_devices = %d, want default %d", d.MaxDevices, defaultMaxDevices)
	}
}

func TestParseConfig_DiscoveryAbsentIsNil(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"target": "10.0.0.1", "mibs": []interface{}{"if-mib"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Discovery != nil {
		t.Errorf("discovery should be nil when absent, got %+v", cfg.Discovery)
	}
}

func TestParseConfig_DiscoveryErrors(t *testing.T) {
	base := func(disc map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"target": "10.0.0.1", "mibs": []interface{}{"if-mib"}, "discovery": disc}
	}
	goodProfile := map[string]interface{}{"version": "v2c", "community": "ro"}
	cases := []struct {
		name    string
		disc    map[string]interface{}
		wantSub string
	}{
		{"no seeds", map[string]interface{}{"profile": goodProfile, "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, "seeds is required"},
		{"bad seed ip", map[string]interface{}{"seeds": []interface{}{"not-an-ip"}, "profile": goodProfile, "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, "not a valid IP"},
		{"no profile", map[string]interface{}{"seeds": []interface{}{"10.0.0.1"}, "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, "discovery.profile is required"},
		{"no community", map[string]interface{}{"seeds": []interface{}{"10.0.0.1"}, "profile": map[string]interface{}{"version": "v2c"}, "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, "community is required"},
		{"v3 crawl profile rejected", map[string]interface{}{"seeds": []interface{}{"10.0.0.1"}, "profile": map[string]interface{}{"version": "v3", "community": "ro"}, "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, "v2c-only"},
		{"no allowed_cidrs", map[string]interface{}{"seeds": []interface{}{"10.0.0.1"}, "profile": goodProfile}, "allowed_cidrs is required"},
		{"bad cidr", map[string]interface{}{"seeds": []interface{}{"10.0.0.1"}, "profile": goodProfile, "allowed_cidrs": []interface{}{"10.0.0.0/99"}}, "not a valid CIDR"},
		{"zero max_devices", map[string]interface{}{"seeds": []interface{}{"10.0.0.1"}, "profile": goodProfile, "allowed_cidrs": []interface{}{"10.0.0.0/8"}, "max_devices": 0}, "max_devices must be positive"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseConfig(base(c.disc))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, want substring %q", err.Error(), c.wantSub)
			}
		})
	}
}

// TestParseConfig_V3 pins the SNMPv3 lot (#156): a valid authPriv
// config resolves, and the invalid shapes fail with actionable errors
// instead of silently downgrading security.
func TestParseConfig_V3(t *testing.T) {
	valid := map[string]interface{}{
		"target":  "192.0.2.10",
		"version": "v3",
		"mibs":    []interface{}{"if-mib"},
		"v3": map[string]interface{}{
			"username":        "monitoring",
			"auth_protocol":   "sha256",
			"auth_passphrase": "auth-secret",
			"priv_protocol":   "aes",
			"priv_passphrase": "priv-secret",
		},
	}
	cfg, err := parseConfig(valid)
	if err != nil {
		t.Fatalf("parseConfig(v3): %v", err)
	}
	if cfg.Version != gosnmp.Version3 || cfg.V3 == nil {
		t.Fatalf("version/V3 not resolved: %+v", cfg)
	}
	if cfg.V3.AuthProtocol != "SHA256" || cfg.V3.PrivProtocol != "AES" {
		t.Errorf("protocol names must be uppercased: %+v", cfg.V3)
	}

	errCases := []struct {
		name string
		mut  func(m map[string]interface{})
		want string
	}{
		{"v3 block without v3 version", func(m map[string]interface{}) { m["version"] = "v2c" }, "version is not v3"},
		{"missing username", func(m map[string]interface{}) {
			m["v3"] = map[string]interface{}{"auth_protocol": "SHA"}
		}, "'v3.username' is required"},
		{"unknown auth protocol", func(m map[string]interface{}) {
			m["v3"] = map[string]interface{}{"username": "u", "auth_protocol": "SHAKE", "auth_passphrase": "x"}
		}, "is not supported"},
		{"priv without auth", func(m map[string]interface{}) {
			m["v3"] = map[string]interface{}{"username": "u", "priv_protocol": "AES", "priv_passphrase": "x"}
		}, "requires an auth_protocol"},
		{"auth without passphrase", func(m map[string]interface{}) {
			m["v3"] = map[string]interface{}{"username": "u", "auth_protocol": "SHA"}
		}, "'v3.auth_passphrase' is required"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{
				"target": "192.0.2.10", "version": "v3", "mibs": []interface{}{"if-mib"},
				"v3": map[string]interface{}{"username": "u"},
			}
			tc.mut(raw)
			_, err := parseConfig(raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
