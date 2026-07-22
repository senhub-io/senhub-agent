package otlpreceiver

import "testing"

func TestParseReceiverConfig_Defaults(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Protocol != protocolGRPC {
		t.Errorf("Protocol = %q, want grpc", cfg.Protocol)
	}
	if cfg.Address != defaultGRPCAddr {
		t.Errorf("Address = %q, want %q", cfg.Address, defaultGRPCAddr)
	}
	if cfg.HTTPPath != defaultHTTPPath {
		t.Errorf("HTTPPath = %q, want %q", cfg.HTTPPath, defaultHTTPPath)
	}
}

func TestParseReceiverConfig_HTTPDefaultAddress(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{"protocol": "http"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Address != defaultHTTPAddr {
		t.Errorf("Address = %q, want %q", cfg.Address, defaultHTTPAddr)
	}
}

func TestParseReceiverConfig_PortOverride(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{"port": 14317})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Address != "127.0.0.1:14317" {
		t.Errorf("Address = %q, want 127.0.0.1:14317", cfg.Address)
	}
}

// TestParseReceiverConfig_DefaultsAreLoopback pins #278: the receiver
// has no authentication, so the listen defaults must not expose it on
// all interfaces — remote ingest is an explicit `address` opt-in.
func TestParseReceiverConfig_DefaultsAreLoopback(t *testing.T) {
	for _, proto := range []string{"grpc", "http"} {
		cfg, err := parseReceiverConfig(map[string]interface{}{"protocol": proto})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", proto, err)
		}
		if got := cfg.Address[:len("127.0.0.1:")]; got != "127.0.0.1:" {
			t.Errorf("%s default Address = %q, want loopback", proto, cfg.Address)
		}
	}
}

func TestParseReceiverConfig_ExplicitAddress(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{"address": "127.0.0.1:5555"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Address != "127.0.0.1:5555" {
		t.Errorf("Address = %q, want 127.0.0.1:5555", cfg.Address)
	}
}

func TestParseReceiverConfig_RejectsBadProtocol(t *testing.T) {
	if _, err := parseReceiverConfig(map[string]interface{}{"protocol": "udp"}); err == nil {
		t.Fatal("expected error for protocol=udp, got nil")
	}
}

func TestParseReceiverConfig_RejectsBadPort(t *testing.T) {
	if _, err := parseReceiverConfig(map[string]interface{}{"port": 70000}); err == nil {
		t.Fatal("expected error for port=70000, got nil")
	}
}

func TestParseReceiverConfig_SignalsDefaultMetricsOnly(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Signals.Metrics || cfg.Signals.Logs {
		t.Errorf("default signals = %+v, want metrics only", cfg.Signals)
	}
}

func TestParseReceiverConfig_SignalsExplicit(t *testing.T) {
	cases := []struct {
		name        string
		signals     []interface{}
		wantMetrics bool
		wantLogs    bool
	}{
		{"logs only", []interface{}{"logs"}, false, true},
		{"metrics and logs", []interface{}{"metrics", "logs"}, true, true},
		{"metrics only", []interface{}{"metrics"}, true, false},
		{"empty falls back to metrics", []interface{}{}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseReceiverConfig(map[string]interface{}{"signals": tc.signals})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Signals.Metrics != tc.wantMetrics || cfg.Signals.Logs != tc.wantLogs {
				t.Errorf("signals = %+v, want metrics=%v logs=%v", cfg.Signals, tc.wantMetrics, tc.wantLogs)
			}
		})
	}
}

func TestParseReceiverConfig_SignalsRejectsTracesAndUnknown(t *testing.T) {
	for _, sig := range []string{"traces", "bogus"} {
		if _, err := parseReceiverConfig(map[string]interface{}{"signals": []interface{}{sig}}); err == nil {
			t.Errorf("signals=[%q] should error", sig)
		}
	}
}

func TestReplacePort(t *testing.T) {
	if got := replacePort("0.0.0.0:4317", 9999); got != "0.0.0.0:9999" {
		t.Errorf("replacePort = %q, want 0.0.0.0:9999", got)
	}
	if got := replacePort("localhost", 1234); got != "localhost:1234" {
		t.Errorf("replacePort(no port) = %q, want localhost:1234", got)
	}
}
