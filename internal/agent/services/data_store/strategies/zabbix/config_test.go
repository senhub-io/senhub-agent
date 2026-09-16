package zabbix

import (
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
)

func TestParseConfigAppliesTheDefaults(t *testing.T) {
	cfg, err := ParseConfig(configuration.StorageConfigParams{"server": "zabbix.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "zabbix.example.com:10051" {
		t.Errorf("server = %q, want the default port appended", cfg.Server)
	}
	if cfg.Hostname == "" {
		t.Error("hostname should default to the machine's name")
	}
	if cfg.HostMetadata != "senhub-agent" || cfg.KeyPrefix != "senhub" {
		t.Errorf("metadata/prefix = %q/%q", cfg.HostMetadata, cfg.KeyPrefix)
	}
	if cfg.Interval != 60*time.Second || cfg.RefreshInterval != 120*time.Second || cfg.HeartbeatInterval != 60*time.Second || cfg.Timeout != 10*time.Second {
		t.Errorf("intervals = %v %v %v %v", cfg.Interval, cfg.RefreshInterval, cfg.HeartbeatInterval, cfg.Timeout)
	}
	if cfg.TLS.Enabled {
		t.Error("tls should be off by default")
	}
}

func TestParseConfigReadsEveryParameter(t *testing.T) {
	cfg, err := ParseConfig(configuration.StorageConfigParams{
		"server": "10.0.0.5:10052", "hostname": "web-01", "host_metadata": "senhub,linux",
		"interval": "30s", "refresh_interval": 300, "heartbeat_interval": "2m", "timeout": 5.5,
		"key_prefix": "acme",
		"tls":        map[string]interface{}{"enabled": true, "ca_file": "/etc/ca.pem", "cert_file": "/etc/c.pem", "key_file": "/etc/k.pem", "server_name": "zbx", "insecure_skip_verify": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "10.0.0.5:10052" || cfg.Hostname != "web-01" || cfg.HostMetadata != "senhub,linux" || cfg.KeyPrefix != "acme" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.Interval != 30*time.Second || cfg.RefreshInterval != 300*time.Second || cfg.HeartbeatInterval != 2*time.Minute || cfg.Timeout != 5500*time.Millisecond {
		t.Errorf("intervals = %v %v %v %v", cfg.Interval, cfg.RefreshInterval, cfg.HeartbeatInterval, cfg.Timeout)
	}
	if !cfg.TLS.Enabled || cfg.TLS.CAFile != "/etc/ca.pem" || cfg.TLS.CertFile != "/etc/c.pem" || cfg.TLS.KeyFile != "/etc/k.pem" || cfg.TLS.ServerName != "zbx" || !cfg.TLS.InsecureSkipVerify {
		t.Errorf("tls = %+v", cfg.TLS)
	}
}

func TestParseConfigRefusesWhatTheAgentWouldRefuse(t *testing.T) {
	cases := []struct {
		name   string
		params configuration.StorageConfigParams
		want   string
	}{
		{"no server", configuration.StorageConfigParams{}, "'server' is required"},
		{"bad server", configuration.StorageConfigParams{"server": "a:b:c"}, "not host:port"},
		{"empty hostname", configuration.StorageConfigParams{"server": "z", "hostname": " "}, "'hostname'"},
		{"metadata too long", configuration.StorageConfigParams{"server": "z", "host_metadata": strings.Repeat("x", 2035)}, "2034"},
		{"bad prefix", configuration.StorageConfigParams{"server": "z", "key_prefix": "sen hub"}, "'key_prefix'"},
		{"bad interval", configuration.StorageConfigParams{"server": "z", "interval": "soon"}, "'interval'"},
		{"zero interval", configuration.StorageConfigParams{"server": "z", "interval": 0}, "must be positive"},
		{"tls not a block", configuration.StorageConfigParams{"server": "z", "tls": true}, "'tls' must be a block"},
		{"cert without key", configuration.StorageConfigParams{"server": "z", "tls": map[string]interface{}{"cert_file": "c.pem"}}, "go together"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseConfig(c.params)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}
