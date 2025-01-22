// internal/agent/probes/syslog/syslogProbe_test.go
package syslog

import (
	"testing"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestParseSyslogProbeConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     map[string]interface{}
		wantPort   int
		wantLabels map[string]string
		wantErr    bool
	}{
		{
			name:       "Default config",
			config:     map[string]interface{}{},
			wantPort:   DefaultSyslogPort,
			wantLabels: map[string]string{},
			wantErr:    false,
		},
		{
			name: "Custom port",
			config: map[string]interface{}{
				"port": float64(2514),
			},
			wantPort:   2514,
			wantLabels: map[string]string{},
			wantErr:    false,
		},
		{
			name: "Invalid port - too low",
			config: map[string]interface{}{
				"port": float64(80),
			},
			wantErr: true,
		},
		{
			name: "Invalid port - too high",
			config: map[string]interface{}{
				"port": float64(70000),
			},
			wantErr: true,
		},
		{
			name: "With labels",
			config: map[string]interface{}{
				"labels": map[string]interface{}{
					"env": "prod",
					"dc":  "paris",
				},
			},
			wantPort: DefaultSyslogPort,
			wantLabels: map[string]string{
				"env": "prod",
				"dc":  "paris",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSyslogProbeConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSyslogProbeConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if got.Port != tt.wantPort {
				t.Errorf("parseSyslogProbeConfig() port = %v, want %v", got.Port, tt.wantPort)
			}

			if len(got.Labels) != len(tt.wantLabels) {
				t.Errorf("parseSyslogProbeConfig() labels count = %v, want %v", len(got.Labels), len(tt.wantLabels))
			}

			for k, v := range tt.wantLabels {
				if got.Labels[k] != v {
					t.Errorf("parseSyslogProbeConfig() label[%s] = %v, want %v", k, got.Labels[k], v)
				}
			}
		})
	}
}

func TestNewSyslogProbe(t *testing.T) {
	log := logger.NewLogger()

	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Valid config",
			config:  map[string]interface{}{},
			wantErr: false,
		},
		{
			name: "Invalid port",
			config: map[string]interface{}{
				"port": float64(80),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSyslogProbe(tt.config, log)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSyslogProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
