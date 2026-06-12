// senhub-agent/internal/agent/probes/syslog/syslogProbe_test.go
package syslog

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
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
			// #278: default bind is loopback — remote senders require
			// an explicit bind_address opt-in (was hardcoded 0.0.0.0).
			name:   "default values",
			config: map[string]interface{}{},
			want: SyslogProbeConfig{
				Port:        DefaultPort,
				Protocol:    DefaultProtocol,
				BindAddress: DefaultBindAddress,
			},
			wantErr: false,
		},
		{
			name: "custom values (json float64 port)",
			config: map[string]interface{}{
				"port":     float64(5140),
				"protocol": "tcp",
			},
			want: SyslogProbeConfig{
				Port:        5140,
				Protocol:    "tcp",
				BindAddress: DefaultBindAddress,
			},
			wantErr: false,
		},
		{
			name: "explicit bind_address opt-in",
			config: map[string]interface{}{
				"bind_address": "0.0.0.0",
			},
			want: SyslogProbeConfig{
				Port:        DefaultPort,
				Protocol:    DefaultProtocol,
				BindAddress: "0.0.0.0",
			},
			wantErr: false,
		},
		// Regression for #136: yaml.v2 decodes integer literals into
		// `int`, not `float64`. Pre-fix the probe only matched float64
		// and silently fell back to DefaultPort for any int-shaped
		// input — i.e. every real YAML config.
		{
			name: "yaml.v2 int port literal",
			config: map[string]interface{}{
				"port": 5140,
			},
			want: SyslogProbeConfig{
				Port:        5140,
				Protocol:    DefaultProtocol,
				BindAddress: DefaultBindAddress,
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
				if got.BindAddress != tt.want.BindAddress {
					t.Errorf("parseSyslogProbeConfig() BindAddress = %v, want %v", got.BindAddress, tt.want.BindAddress)
				}
			}
		})
	}
}

// TestProcessLogMessage_RFC3164_And_RFC5424_Fallback pins the #135
// contract: the go-syslog v2 library populates {content, tag} for
// RFC3164 messages and {message, app_name} for RFC5424 messages, so
// the probe must read both shapes. Pre-fix, RFC5424 traffic landed
// with an empty body and an empty appname tag.
func TestProcessLogMessage_RFC3164_And_RFC5424_Fallback(t *testing.T) {
	zlog := zerolog.New(os.Stderr)
	base := logger.NewModuleLogger((*logger.Logger)(&zlog), "probe.syslog.test")

	cases := []struct {
		name        string
		logParts    map[string]interface{}
		wantMessage string
		wantTag     string
	}{
		{
			name: "RFC3164 shape — content + tag populated",
			logParts: map[string]interface{}{
				"facility":  1,
				"severity":  6,
				"hostname":  "node-1",
				"client":    "10.0.0.1:514",
				"priority":  14,
				"timestamp": time.Now(),
				"content":   "hello from rfc3164",
				"tag":       "myapp",
			},
			wantMessage: "hello from rfc3164",
			wantTag:     "myapp",
		},
		{
			name: "RFC5424 shape — message + app_name populated, content + tag absent",
			logParts: map[string]interface{}{
				"facility":  1,
				"severity":  6,
				"hostname":  "node-2",
				"client":    "10.0.0.2:514",
				"priority":  14,
				"timestamp": time.Now(),
				"message":   "hello from rfc5424",
				"app_name":  "newapp",
			},
			wantMessage: "hello from rfc5424",
			wantTag:     "newapp",
		},
		{
			name: "RFC3164 keys win when both shapes coexist",
			logParts: map[string]interface{}{
				"facility":  1,
				"severity":  6,
				"hostname":  "node-3",
				"client":    "10.0.0.3:514",
				"priority":  14,
				"timestamp": time.Now(),
				"content":   "rfc3164 body",
				"tag":       "rfc3164app",
				"message":   "rfc5424 body",
				"app_name":  "rfc5424app",
			},
			wantMessage: "rfc3164 body",
			wantTag:     "rfc3164app",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured []data_store.DataPoint
			probe := &SyslogProbe{
				BaseProbe:    &types.BaseProbe{},
				config:       SyslogProbeConfig{Port: DefaultPort, Protocol: DefaultProtocol},
				moduleLogger: base,
				callback: func(points []data_store.DataPoint) error {
					captured = append(captured, points...)
					return nil
				},
			}

			probe.processLogMessage(tc.logParts)

			if len(captured) != 1 {
				t.Fatalf("expected 1 datapoint, got %d", len(captured))
			}
			dp := captured[0]
			var gotMessage, gotTag string
			for _, tag := range dp.Tags {
				switch tag.Key {
				case "message":
					gotMessage = tag.Value
				case "tag":
					gotTag = tag.Value
				}
			}
			if gotMessage != tc.wantMessage {
				t.Errorf("message tag = %q, want %q", gotMessage, tc.wantMessage)
			}
			if gotTag != tc.wantTag {
				t.Errorf("tag tag = %q, want %q", gotTag, tc.wantTag)
			}
		})
	}
}
