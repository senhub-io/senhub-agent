package oracle

import (
	"testing"
	"time"
)

func TestParseConfig_RequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		raw    map[string]interface{}
		errMsg string
	}{
		{
			name:   "missing host",
			raw:    map[string]interface{}{"service_name": "XE", "username": "mon"},
			errMsg: "host",
		},
		{
			name:   "empty host",
			raw:    map[string]interface{}{"host": "", "service_name": "XE", "username": "mon"},
			errMsg: "host",
		},
		{
			name:   "missing service_name",
			raw:    map[string]interface{}{"host": "db.local", "username": "mon"},
			errMsg: "service_name",
		},
		{
			name:   "missing username",
			raw:    map[string]interface{}{"host": "db.local", "service_name": "XE"},
			errMsg: "username",
		},
		{
			name:   "port out of range",
			raw:    map[string]interface{}{"host": "db.local", "service_name": "XE", "username": "mon", "port": 99999},
			errMsg: "port",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.raw)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, defaultPort)
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", cfg.Interval, defaultInterval)
	}
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Password)
	}
}

func TestParseConfig_Overrides(t *testing.T) {
	raw := map[string]interface{}{
		"host":         "oracle.local",
		"service_name": "ORCL",
		"username":     "monitor",
		"password":     "s3cr3t",
		"port":         float64(1522),
		"interval":     float64(120),
	}
	cfg, err := parseConfig(raw)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Port != 1522 {
		t.Errorf("Port = %d, want 1522", cfg.Port)
	}
	if cfg.Interval != 120*time.Second {
		t.Errorf("Interval = %v, want 120s", cfg.Interval)
	}
	if cfg.Password != "s3cr3t" {
		t.Errorf("Password = %q, want s3cr3t", cfg.Password)
	}
}

func TestConfig_Instance(t *testing.T) {
	cfg := config{Host: "db.example.com", Port: 1521, ServiceName: "ORCL"}
	want := "oracle://db.example.com:1521/ORCL"
	if got := cfg.instance(); got != want {
		t.Errorf("instance() = %q, want %q", got, want)
	}
}
