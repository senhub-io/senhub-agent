// senhub-agent/internal/agent/services/data_store/strategies/http/http_lookups_test.go
package http

import (
	"testing"

	"github.com/rs/zerolog"
)

// TestNewLookupRegistry tests the creation of a new lookup registry
func TestNewLookupRegistry(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)

	if err != nil {
		t.Fatalf("NewLookupRegistry() failed: %v", err)
	}

	if registry == nil {
		t.Fatal("NewLookupRegistry() returned nil registry")
	}

	if registry.lookups == nil {
		t.Fatal("Registry lookups map is nil")
	}
}

// TestLookupRegistry_GetLookup tests retrieving lookups by ID
func TestLookupRegistry_GetLookup(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Test getting an existing lookup (netscaler.lbvserver.state should exist)
	lookup, found := registry.GetLookup("netscaler.lbvserver.state")
	if !found {
		t.Error("Expected to find netscaler.lbvserver.state lookup, but it was not found")
	}

	if lookup.ID != "netscaler.lbvserver.state" {
		t.Errorf("Expected lookup ID 'netscaler.lbvserver.state', got '%s'", lookup.ID)
	}

	if lookup.Description == "" {
		t.Error("Lookup description should not be empty")
	}

	// Test getting a non-existent lookup
	_, found = registry.GetLookup("nonexistent.lookup")
	if found {
		t.Error("Expected not to find nonexistent lookup, but it was found")
	}
}

// TestLookupRegistry_GetAllLookups tests retrieving all lookups
func TestLookupRegistry_GetAllLookups(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	allLookups := registry.GetAllLookups()

	if len(allLookups) == 0 {
		t.Error("Expected at least one lookup, got none")
	}

	// Verify all lookups have required fields
	for id, lookup := range allLookups {
		if lookup.ID != id {
			t.Errorf("Lookup ID mismatch: key=%s, lookup.ID=%s", id, lookup.ID)
		}

		if lookup.Description == "" {
			t.Errorf("Lookup %s has empty description", id)
		}

		if lookup.Type == "" {
			t.Errorf("Lookup %s has empty type", id)
		}

		if len(lookup.Mappings) == 0 {
			t.Errorf("Lookup %s has no mappings", id)
		}
	}
}

// TestLookupRegistry_GetLookupsForProbe tests retrieving lookups for a specific probe
func TestLookupRegistry_GetLookupsForProbe(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Test getting Netscaler lookups
	netscalerLookups := registry.GetLookupsForProbe("netscaler")

	if len(netscalerLookups) == 0 {
		t.Error("Expected at least one Netscaler lookup, got none")
	}

	// Verify all returned lookups are for Netscaler
	for id := range netscalerLookups {
		if len(id) < 10 || id[:10] != "netscaler." {
			t.Errorf("Lookup %s does not start with 'netscaler.'", id)
		}
	}

	// Test getting lookups for non-existent probe
	nonExistentLookups := registry.GetLookupsForProbe("nonexistent")
	if len(nonExistentLookups) != 0 {
		t.Error("Expected zero lookups for nonexistent probe, got some")
	}
}

// TestLookupDefinition_Mappings tests that lookup mappings have correct structure
func TestLookupDefinition_Mappings(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Test netscaler.lbvserver.state mappings
	lookup, found := registry.GetLookup("netscaler.lbvserver.state")
	if !found {
		t.Fatal("netscaler.lbvserver.state lookup not found")
	}

	// Verify official Citrix ADC state codes
	expectedMappings := map[int]string{
		1: "DOWN",
		2: "UNKNOWN",
		3: "BUSY",
		4: "OUT OF SERVICE",
		5: "TROFS",
		7: "UP",
		8: "TROFS_DOWN",
	}

	for code, expectedText := range expectedMappings {
		mapping, exists := lookup.Mappings[code]
		if !exists {
			t.Errorf("Expected mapping for code %d, but it doesn't exist", code)
			continue
		}

		if mapping.Text != expectedText {
			t.Errorf("Code %d: expected text '%s', got '%s'", code, expectedText, mapping.Text)
		}

		if mapping.Severity == "" {
			t.Errorf("Code %d: severity should not be empty", code)
		}

		// Verify severity is one of the allowed values
		validSeverities := map[string]bool{
			"ok":      true,
			"warning": true,
			"error":   true,
			"unknown": true,
		}

		if !validSeverities[mapping.Severity] {
			t.Errorf("Code %d: invalid severity '%s'", code, mapping.Severity)
		}
	}

	// Verify desired_value is set and valid
	if lookup.DesiredValue == 0 {
		t.Error("DesiredValue should be set for lbvserver.state")
	}

	if lookup.DesiredValue != 7 {
		t.Errorf("Expected desired_value=7 (UP), got %d", lookup.DesiredValue)
	}
}

// TestLookupRegistry_ThreadSafety tests concurrent access to the registry
func TestLookupRegistry_ThreadSafety(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Simulate concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				registry.GetLookup("netscaler.lbvserver.state")
				registry.GetAllLookups()
				registry.GetLookupsForProbe("netscaler")
			}
			done <- true
		}()
	}

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestLookupRegistry_EmbeddedYAMLLoading tests that embedded YAML is loaded correctly
func TestLookupRegistry_EmbeddedYAMLLoading(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	// Verify that at least the documented Netscaler lookups are present
	expectedLookups := []string{
		"netscaler.lbvserver.state",
		"netscaler.service.state",
		"netscaler.servicegroup.state",
		"netscaler.interface.state",
		"netscaler.ssl.certificate.status",
		"netscaler.gslbvserver.state",
		"netscaler.csvserver.state",
	}

	for _, lookupID := range expectedLookups {
		_, found := registry.GetLookup(lookupID)
		if !found {
			t.Errorf("Expected lookup %s not found in registry", lookupID)
		}
	}
}

// TestLookupValue_Fields tests that all lookup values have required fields
func TestLookupValue_Fields(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	allLookups := registry.GetAllLookups()

	for lookupID, lookup := range allLookups {
		for code, value := range lookup.Mappings {
			// Text field is mandatory
			if value.Text == "" {
				t.Errorf("Lookup %s, code %d: Text field is empty", lookupID, code)
			}

			// Severity field is mandatory
			if value.Severity == "" {
				t.Errorf("Lookup %s, code %d: Severity field is empty", lookupID, code)
			}

			// Description is optional, but if present should not be just whitespace
			if value.Description != "" && len(value.Description) < 3 {
				t.Errorf("Lookup %s, code %d: Description too short: '%s'", lookupID, code, value.Description)
			}
		}
	}
}

// BenchmarkGetLookup benchmarks the GetLookup operation
func BenchmarkGetLookup(b *testing.B) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		b.Fatalf("Failed to create registry: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry.GetLookup("netscaler.lbvserver.state")
	}
}

// BenchmarkGetAllLookups benchmarks the GetAllLookups operation
func BenchmarkGetAllLookups(b *testing.B) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		b.Fatalf("Failed to create registry: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry.GetAllLookups()
	}
}
