package otlp

import (
	"testing"
	"time"
)

func TestParseConfig_KeepaliveInterval(t *testing.T) {
	cases := []struct {
		name    string
		raw     any
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: DefaultKeepaliveInterval},
		{name: "set", raw: "2m", want: 2 * time.Minute},
		{name: "off", raw: "0s", want: 0},
		{name: "negative", raw: "-1m", wantErr: true},
		{name: "not a duration", raw: "soon", wantErr: true},
		{name: "not a string", raw: 240, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]interface{}{"endpoint": "otlp.example.com:4317"}
			if tc.raw != nil {
				params["keepalive_interval"] = tc.raw
			}
			cfg, err := ParseConfig(params)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("keepalive_interval %v accepted", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.KeepaliveInterval != tc.want {
				t.Errorf("KeepaliveInterval = %v, want %v", cfg.KeepaliveInterval, tc.want)
			}
		})
	}
}

func TestKeepaliveOutlastsStaleness(t *testing.T) {
	cases := []struct {
		keepalive, staleness string
		want                 bool
	}{
		{"4m", "10m", false},
		{"10m", "10m", true},
		{"15m", "10m", true},
		{"0s", "10m", false},
		{"15m", "0s", false},
	}
	for _, tc := range cases {
		params := map[string]interface{}{
			"endpoint":           "otlp.example.com:4317",
			"keepalive_interval": tc.keepalive,
			"staleness_ttl":      tc.staleness,
		}
		if got := KeepaliveOutlastsStaleness(params); got != tc.want {
			t.Errorf("keepalive %s, staleness %s: got %v, want %v", tc.keepalive, tc.staleness, got, tc.want)
		}
	}
}
