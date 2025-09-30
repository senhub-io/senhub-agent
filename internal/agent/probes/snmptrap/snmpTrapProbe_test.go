package snmptrap

import (
	"context"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestNewSNMPTrapProbe tests probe creation
func TestNewSNMPTrapProbe(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	tests := []struct {
		name    string
		params  interface{}
		wantErr bool
	}{
		{
			name: "valid configuration",
			params: map[string]interface{}{
				"listen_address": "127.0.0.1:162",
				"buffer_size":    float64(1000),
				"communities":    []interface{}{"public", "private"},
			},
			wantErr: false,
		},
		{
			name: "minimal configuration",
			params: map[string]interface{}{},
			wantErr: false,
		},
		{
			name: "invalid buffer size",
			params: map[string]interface{}{
				"buffer_size": float64(50), // Below minimum
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, err := NewSNMPTrapProbe(tt.params.(map[string]interface{}), baseLogger)
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			
			if probe == nil {
				t.Error("Expected probe instance but got nil")
			}
			
			// Test probe type
			snmpProbe, ok := probe.(*SNMPTrapProbe)
			if !ok {
				t.Error("Expected *SNMPTrapProbe type")
			}
			
			if snmpProbe.config == nil {
				t.Error("Expected config to be initialized")
			}
			
			if snmpProbe.buffer == nil {
				t.Error("Expected buffer to be initialized")
			}
		})
	}
}

// TestSNMPTrapProbe_Configuration tests configuration parsing
func TestSNMPTrapProbe_Configuration(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	params := map[string]interface{}{
		"listen_address": "0.0.0.0:1162",
		"buffer_size":    float64(2000),
		"communities":    []interface{}{"test", "monitoring"},
		"mib_enrichment": map[string]interface{}{
			"enabled":     true,
			"cache_size":  float64(5000),
			"cache_ttl":   "12h",
		},
		"filters": map[string]interface{}{
			"allowed_sources": []interface{}{"192.168.1.0/24", "10.0.0.0/8"},
			"blocked_sources": []interface{}{"192.168.1.100"},
		},
	}
	
	probe, err := NewSNMPTrapProbe(params, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}
	
	snmpProbe := probe.(*SNMPTrapProbe)
	config := snmpProbe.config
	
	// Test basic configuration
	if config.ListenAddress != "0.0.0.0:1162" {
		t.Errorf("Expected listen address '0.0.0.0:1162', got '%s'", config.ListenAddress)
	}
	
	if config.BufferSize != 2000 {
		t.Errorf("Expected buffer size 2000, got %d", config.BufferSize)
	}
	
	// Test communities
	if len(config.Communities) != 2 {
		t.Errorf("Expected 2 communities, got %d", len(config.Communities))
	}
	
	// Test MIB configuration
	if !config.MIBEnrichment.Enabled {
		t.Error("Expected MIB enrichment to be enabled")
	}
	
	if config.MIBEnrichment.CacheSize != 5000 {
		t.Errorf("Expected MIB cache size 5000, got %d", config.MIBEnrichment.CacheSize)
	}
	
	// Test filters
	if len(config.Filters.AllowedSources) != 2 {
		t.Errorf("Expected 2 allowed sources, got %d", len(config.Filters.AllowedSources))
	}
	
	if len(config.Filters.BlockedSources) != 1 {
		t.Errorf("Expected 1 blocked source, got %d", len(config.Filters.BlockedSources))
	}
}

// TestSNMPTrapProbe_StartStop tests probe lifecycle
func TestSNMPTrapProbe_StartStop(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	params := map[string]interface{}{
		"listen_address": "127.0.0.1:0", // Use port 0 for dynamic assignment
		"buffer_size":    float64(100),
	}
	
	probe, err := NewSNMPTrapProbe(params, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}
	
	snmpProbe := probe.(*SNMPTrapProbe)
	
	// Test initial state
	if snmpProbe.running {
		t.Error("Probe should not be running initially")
	}
	
	// Note: We skip actual Start() test here because it requires binding to UDP port
	// which might fail in test environments. The integration tests will cover this.
	
	// Test shutdown without starting (should not error)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err = snmpProbe.OnShutdown(ctx)
	if err != nil {
		t.Errorf("Unexpected error during shutdown: %v", err)
	}
}

// TestSNMPTrapProbe_CollectData tests data collection
func TestSNMPTrapProbe_CollectData(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	probe, err := NewSNMPTrapProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}
	
	snmpProbe := probe.(*SNMPTrapProbe)
	
	// Event-driven probes should always return empty results from Collect()
	metrics, err := snmpProbe.Collect()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	
	if len(metrics) != 0 {
		t.Errorf("Expected 0 metrics for event-driven probe, got %d", len(metrics))
	}
	
	// Test that GetInterval returns 0 for event-driven probes
	if interval := snmpProbe.GetInterval(); interval != 0 {
		t.Errorf("Expected interval 0 for event-driven probe, got %v", interval)
	}
}

// TestHandleTrap tests trap handling logic
func TestHandleTrap(t *testing.T) {
	baseLogger := &logger.Logger{}
	
	probe, err := NewSNMPTrapProbe(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("Failed to create probe: %v", err)
	}
	
	snmpProbe := probe.(*SNMPTrapProbe)
	
	// Create a test packet
	packet := &gosnmp.SnmpPacket{
		Version:      gosnmp.Version2c,
		Community:    "public",
		Variables: []gosnmp.SnmpPDU{
			{
				Name:  ".1.3.6.1.2.1.1.3.0",
				Type:  gosnmp.TimeTicks,
				Value: uint32(1000),
			},
			{
				Name:  ".1.3.6.1.6.3.1.1.4.1.0",
				Type:  gosnmp.ObjectIdentifier,
				Value: ".1.3.6.1.6.3.1.1.5.3",
			},
		},
	}
	// Set enterprise field for SNMPv1 compatibility
	packet.Enterprise = "1.3.6.1.6.3.1.1.5"
	
	// Test trap handling
	initialBufferSize := snmpProbe.buffer.Size()
	snmpProbe.handleTrap(packet, "192.168.1.100:45678")
	
	if snmpProbe.buffer.Size() != initialBufferSize+1 {
		t.Errorf("Expected buffer size to increase by 1, got %d", snmpProbe.buffer.Size())
	}
	
	// Test statistics
	if snmpProbe.stats.trapsReceived != 1 {
		t.Errorf("Expected traps received count 1, got %d", snmpProbe.stats.trapsReceived)
	}
}