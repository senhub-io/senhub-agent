package gateway

import (
	"context"
	"testing"
	"time"

	"senhub-agent.go/probesdk/cliargs"
	"senhub-agent.go/probesdk/logger"
)

func TestNewPingGatewayProbe(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{"Valid probe", map[string]interface{}{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewPingGatewayProbe(tt.config, baseLogger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPingGatewayProbe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				// Test BaseProbe inheritance: SetName() and GetName()
				probe.(interface{ SetName(string) }).SetName("ping_gateway")
				if probe.GetName() != "ping_gateway" {
					t.Errorf("Expected name 'ping_gateway', got '%s'", probe.GetName())
				}
			}
		})
	}
}

func TestPingGatewayProbe_GetName(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)

	// Test BaseProbe inheritance: SetName() and GetName()
	probe.(interface{ SetName(string) }).SetName("ping_gateway")
	if probe.GetName() != "ping_gateway" {
		t.Errorf("GetName() = %s, want 'ping_gateway'", probe.GetName())
	}

	// Test default behavior: GetName() returns empty string before SetName() is called
	probe2, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)
	if probe2.GetName() != "" {
		t.Errorf("GetName() before SetName() = %s, want empty string", probe2.GetName())
	}
}

func TestPingGatewayProbe_GetInterval(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)
	expected := 30 * time.Second
	if probe.GetInterval() != expected {
		t.Errorf("GetInterval() = %v, want %v", probe.GetInterval(), expected)
	}
}

func TestPingGatewayProbe_ShouldStart(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)
	if !probe.ShouldStart() {
		t.Error("ShouldStart() should return true")
	}
}

func TestPingGatewayProbe_GetTargetStrategies(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)
	pgProbe := probe.(*PingGatewayProbe)
	strategies := pgProbe.GetTargetStrategies()
	expected := []string{"senhub", "prtg", "http", "otlp"}

	if len(strategies) != len(expected) {
		t.Errorf("GetTargetStrategies() returned %d strategies, want %d", len(strategies), len(expected))
	}
}

func TestPingGatewayProbe_OnStart(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)
	quitChannel := make(chan struct{})

	if err := probe.OnStart(quitChannel); err != nil {
		t.Errorf("OnStart() should not return error, got: %v", err)
	}
}

func TestPingGatewayProbe_OnShutdown(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)
	ctx := context.Background()

	if err := probe.OnShutdown(ctx); err != nil {
		t.Errorf("OnShutdown() should not return error, got: %v", err)
	}
}

func TestValidateIPAddress(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		wantErr bool
	}{
		{"Valid IPv4", "192.168.1.1", false},
		{"Valid IPv4 localhost", "127.0.0.1", false},
		{"Valid IPv4 public", "8.8.8.8", false},
		{"Valid IPv6", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", false},
		{"Valid IPv6 localhost", "::1", false},
		{"Invalid IP empty", "", true},
		{"Invalid IP malformed", "192.168.1", true},
		{"Invalid IP with special chars", "192.168.1.1; rm -rf", true},
		{"Invalid IP with pipe", "192.168.1.1 | cat /etc/passwd", true},
		{"Invalid IP with redirect", "192.168.1.1 > /tmp/hack", true},
		{"Invalid IP with backtick", "192.168.1.1`whoami`", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIPAddress(tt.ip)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIPAddress(%s) error = %v, wantErr %v", tt.ip, err, tt.wantErr)
			}
		})
	}
}

func TestPingGatewayProbe_Collect(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	probe, _ := NewPingGatewayProbe(map[string]interface{}{}, baseLogger)

	// Just test that Collect doesn't crash
	// It may fail if no network or gateway available, which is expected in test environments
	_, err := probe.Collect()

	// We don't assert on error because:
	// - CI environments may not have network access
	// - Docker containers may not have a gateway
	// - This is integration-level testing, not unit testing
	if err != nil {
		t.Logf("Collect() returned expected error in test environment: %v", err)
	}
}
