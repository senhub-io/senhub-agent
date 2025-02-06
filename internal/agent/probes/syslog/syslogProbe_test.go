// senhub-agent/internal/agent/probes/syslog/syslogProbe_test.go
package syslog

import (
	"github.com/rs/zerolog"
	"os"
	"testing"
)

func TestNewSyslogProbe(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name: "Valid Probe",
			config: map[string]interface{}{
				"port":     float64(514),
				"protocol": "udp",
			},
			wantErr: false,
		},
		{
			name:    "Valid Probe with defaults",
			config:  map[string]interface{}{},
			wantErr: false,
		},
		{
			name: "Invalid port",
			config: map[string]interface{}{
				"port": float64(70000),
			},
			wantErr: true,
		},
		{
			name: "Invalid protocol",
			config: map[string]interface{}{
				"protocol": "http",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSyslogProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSyslogProbe() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSyslogProbeConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  map[string]interface{}
		want    SyslogProbeConfig
		wantErr bool
	}{
		{
			name:   "default values",
			config: map[string]interface{}{},
			want: SyslogProbeConfig{
				Port:     DefaultPort,
				Protocol: DefaultProtocol,
			},
			wantErr: false,
		},
		{
			name: "custom values",
			config: map[string]interface{}{
				"port":     float64(5140),
				"protocol": "tcp",
			},
			want: SyslogProbeConfig{
				Port:     5140,
				Protocol: "tcp",
			},
			wantErr: false,
		},
		{
			name: "invalid port",
			config: map[string]interface{}{
				"port": float64(70000),
			},
			wantErr: true,
		},
		{
			name: "invalid protocol",
			config: map[string]interface{}{
				"protocol": "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSyslogProbeConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSyslogProbeConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Port != tt.want.Port {
					t.Errorf("parseSyslogProbeConfig() Port = %v, want %v", got.Port, tt.want.Port)
				}
				if got.Protocol != tt.want.Protocol {
					t.Errorf("parseSyslogProbeConfig() Protocol = %v, want %v", got.Protocol, tt.want.Protocol)
				}
			}
		})
	}
}
