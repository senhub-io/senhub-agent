package otlp

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestSplitEndpointScheme(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantHost string
		wantTLS  *bool
		wantErr  string
	}{
		{name: "host:port unchanged", raw: "collector:4318", wantHost: "collector:4318", wantTLS: nil},
		{name: "http", raw: "http://h:4318", wantHost: "h:4318", wantTLS: boolPtr(false)},
		{name: "https trailing slash", raw: "https://h:4318/", wantHost: "h:4318", wantTLS: boolPtr(true)},
		{name: "userinfo rejected", raw: "http://user:pass@h:4318", wantErr: "credentials"},
		{name: "grpc scheme rejected", raw: "grpc://h:4317", wantErr: "scheme"},
		{name: "unix scheme rejected", raw: "unix:///tmp/sock", wantErr: "scheme"},
		{name: "path rejected", raw: "http://h:4318/v1/metrics", wantErr: "host:port"},
		{name: "query rejected", raw: "http://h:4318?x=1", wantErr: "host:port"},
		{name: "fragment rejected", raw: "http://h:4318#f", wantErr: "host:port"},
		{name: "empty host rejected", raw: "http://:4318", wantErr: "host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, tls, err := splitEndpointScheme(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err=%v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tt.wantHost {
				t.Errorf("hostPort=%q, want %q", host, tt.wantHost)
			}
			if (tls == nil) != (tt.wantTLS == nil) {
				t.Fatalf("tls=%v, want %v", tls, tt.wantTLS)
			}
			if tls != nil && *tls != *tt.wantTLS {
				t.Errorf("tls=%v, want %v", *tls, *tt.wantTLS)
			}
		})
	}
}

func TestParseConfig_EndpointScheme(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]interface{}
		wantErr string
		check   func(t *testing.T, cfg Config)
	}{
		{
			name:   "host:port unchanged, TLS default",
			params: map[string]interface{}{"endpoint": "h:4318"},
			check: func(t *testing.T, cfg Config) {
				if cfg.Endpoint != "h:4318" {
					t.Errorf("Endpoint=%q", cfg.Endpoint)
				}
				if !cfg.TLS.Enabled {
					t.Error("TLS.Enabled should stay true")
				}
			},
		},
		{
			name:   "http scheme strips and disables TLS",
			params: map[string]interface{}{"endpoint": "http://h:4318"},
			check: func(t *testing.T, cfg Config) {
				if cfg.Endpoint != "h:4318" {
					t.Errorf("Endpoint=%q", cfg.Endpoint)
				}
				if cfg.TLS.Enabled {
					t.Error("TLS.Enabled should be false")
				}
			},
		},
		{
			name:   "https scheme with trailing slash",
			params: map[string]interface{}{"endpoint": "https://h:4318/"},
			check: func(t *testing.T, cfg Config) {
				if cfg.Endpoint != "h:4318" {
					t.Errorf("Endpoint=%q", cfg.Endpoint)
				}
				if !cfg.TLS.Enabled {
					t.Error("TLS.Enabled should be true")
				}
			},
		},
		{
			name: "http scheme conflicts with explicit tls.enabled true",
			params: map[string]interface{}{
				"endpoint": "http://h:4318",
				"tls":      map[string]interface{}{"enabled": true},
			},
			wantErr: "tls.enabled",
		},
		{
			name: "https scheme conflicts with explicit tls.enabled false",
			params: map[string]interface{}{
				"endpoint": "https://h:4318",
				"tls":      map[string]interface{}{"enabled": false},
			},
			wantErr: "tls.enabled",
		},
		{
			name:    "userinfo rejected",
			params:  map[string]interface{}{"endpoint": "http://user:pass@h:4318"},
			wantErr: "credentials",
		},
		{
			name:    "grpc scheme rejected",
			params:  map[string]interface{}{"endpoint": "grpc://h:4317"},
			wantErr: "scheme",
		},
		{
			name:    "URL path rejected",
			params:  map[string]interface{}{"endpoint": "http://h:4318/v1/metrics"},
			wantErr: "host:port",
		},
		{
			name: "signal endpoint scheme disagrees with root TLS outcome",
			params: map[string]interface{}{
				"endpoint": "https://a:4318",
				"signals": map[string]interface{}{
					"metrics": map[string]interface{}{"endpoint": "http://m:4318"},
				},
			},
			wantErr: "metrics.endpoint",
		},
		{
			name: "signal endpoint with own tls block accepted",
			params: map[string]interface{}{
				"endpoint": "https://a:4318",
				"signals": map[string]interface{}{
					"metrics": map[string]interface{}{
						"endpoint": "http://m:4318",
						"tls":      map[string]interface{}{"enabled": false},
					},
				},
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.Metrics.Endpoint != "m:4318" {
					t.Errorf("Metrics.Endpoint=%q", cfg.Metrics.Endpoint)
				}
				if cfg.Metrics.TLS == nil || cfg.Metrics.TLS.Enabled {
					t.Errorf("Metrics.TLS=%+v, want enabled=false", cfg.Metrics.TLS)
				}
			},
		},
		{
			name: "fallback endpoint scheme agrees with root http",
			params: map[string]interface{}{
				"endpoint":           "http://a:4318",
				"fallback_endpoints": []interface{}{"http://b:4318"},
			},
			check: func(t *testing.T, cfg Config) {
				if len(cfg.FallbackEndpoints) != 1 || cfg.FallbackEndpoints[0] != "b:4318" {
					t.Errorf("FallbackEndpoints=%v", cfg.FallbackEndpoints)
				}
			},
		},
		{
			name: "fallback endpoint scheme disagrees with root https",
			params: map[string]interface{}{
				"endpoint":           "https://a:4318",
				"fallback_endpoints": []interface{}{"http://b:4318"},
			},
			wantErr: "fallback_endpoints",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig(tt.params)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err=%v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestParseConfig_EndpointUserinfoNotEchoed(t *testing.T) {
	_, err := ParseConfig(map[string]interface{}{
		"endpoint": "http://user:pass@h:4318",
	})
	if err == nil {
		t.Fatal("expected an error for credentials in the endpoint")
	}
	if strings.Contains(err.Error(), "user:pass") || strings.Contains(err.Error(), "pass") {
		t.Fatalf("error echoes credentials: %v", err)
	}
}
