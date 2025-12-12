// senhub-agent/internal/agent/services/data_store/strategies/http/http_lookups_prtg_test.go
package http

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// TestNewPRTGLookupGenerator tests PRTG generator creation
func TestNewPRTGLookupGenerator(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	if generator == nil {
		t.Fatal("Expected non-nil generator")
	}

	if generator.registry == nil {
		t.Fatal("Generator registry should not be nil")
	}
}

// TestGenerateXML tests XML generation for a specific lookup
func TestGenerateXML(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	// Test generating XML for netscaler.lbvserver.state
	xmlOutput, err := generator.GenerateXML("netscaler.lbvserver.state")
	if err != nil {
		t.Fatalf("GenerateXML failed: %v", err)
	}

	// Verify XML header
	if !strings.HasPrefix(xmlOutput, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("XML should start with XML declaration")
	}

	// Verify XML contains lookup ID
	if !strings.Contains(xmlOutput, `id="netscaler.lbvserver.state"`) {
		t.Error("XML should contain lookup ID attribute")
	}

	// Verify XML contains desired value
	if !strings.Contains(xmlOutput, `desiredValue="7"`) {
		t.Error("XML should contain desiredValue attribute (7 for Up)")
	}

	// Verify XML contains state mappings (capitalized)
	expectedStates := []string{
		"Down",
		"Unknown",
		"Busy",
		"Out of service",
		"Trofs",
		"Up",
		"Trofs_down",
	}

	for _, state := range expectedStates {
		if !strings.Contains(xmlOutput, state) {
			t.Errorf("XML should contain state '%s'", state)
		}
	}

	// Verify XML is valid by unmarshaling
	var prtgLookup PRTGValueLookup
	// Remove XML header for unmarshaling
	xmlContent := strings.TrimPrefix(xmlOutput, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	if err := xml.Unmarshal([]byte(xmlContent), &prtgLookup); err != nil {
		t.Errorf("Generated XML is not valid: %v", err)
	}

	// Verify unmarshaled structure
	if prtgLookup.ID != "netscaler.lbvserver.state" {
		t.Errorf("Expected ID 'netscaler.lbvserver.state', got '%s'", prtgLookup.ID)
	}

	if prtgLookup.DesiredValue != 7 {
		t.Errorf("Expected desiredValue 7, got %d", prtgLookup.DesiredValue)
	}

	if len(prtgLookup.Lookups.Entries) != 7 {
		t.Errorf("Expected 7 entries, got %d", len(prtgLookup.Lookups.Entries))
	}
}

// TestGenerateXML_NonExistentLookup tests error handling for non-existent lookup
func TestGenerateXML_NonExistentLookup(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	_, err = generator.GenerateXML("nonexistent.lookup")
	if err == nil {
		t.Error("Expected error for non-existent lookup")
	}

	if !strings.Contains(err.Error(), "lookup not found") {
		t.Errorf("Error should mention 'lookup not found', got: %v", err)
	}
}

// TestGenerateAllXML tests generating XML for all lookups
func TestGenerateAllXML(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	xmlFiles, err := generator.GenerateAllXML()
	if err != nil {
		t.Fatalf("GenerateAllXML failed: %v", err)
	}

	// Should have generated XML for all lookups
	allLookups := registry.GetAllLookups()
	if len(xmlFiles) != len(allLookups) {
		t.Errorf("Expected %d XML files, got %d", len(allLookups), len(xmlFiles))
	}

	// Verify each lookup has valid XML
	for lookupID, xmlContent := range xmlFiles {
		if !strings.HasPrefix(xmlContent, `<?xml version="1.0" encoding="UTF-8"?>`) {
			t.Errorf("XML for %s should start with XML declaration", lookupID)
		}

		if !strings.Contains(xmlContent, lookupID) {
			t.Errorf("XML for %s should contain its lookup ID", lookupID)
		}
	}
}

// TestGenerateXMLForProbe tests generating XML for probe-specific lookups
func TestGenerateXMLForProbe(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	// Test generating Netscaler lookups
	xmlFiles, err := generator.GenerateXMLForProbe("netscaler")
	if err != nil {
		t.Fatalf("GenerateXMLForProbe failed: %v", err)
	}

	// Should have at least one Netscaler lookup
	if len(xmlFiles) == 0 {
		t.Error("Expected at least one Netscaler lookup XML")
	}

	// Verify all returned lookups are for Netscaler
	for lookupID := range xmlFiles {
		if !strings.HasPrefix(lookupID, "netscaler.") {
			t.Errorf("Lookup %s should start with 'netscaler.'", lookupID)
		}
	}
}

// TestGenerateXMLForProbe_NonExistent tests error handling for non-existent probe
func TestGenerateXMLForProbe_NonExistent(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	xmlFiles, err := generator.GenerateXMLForProbe("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return empty map for non-existent probe
	if len(xmlFiles) != 0 {
		t.Error("Expected empty map for non-existent probe")
	}
}

// TestGetFilenameForLookup tests filename generation
func TestGetFilenameForLookup(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	tests := []struct {
		name       string
		lookupID   string
		expected   string
	}{
		{
			name:     "Netscaler lbvserver state",
			lookupID: "netscaler.lbvserver.state",
			expected: "prtg.valuelookup.netscaler_lbvserver_state.ovl",
		},
		{
			name:     "Netscaler service state",
			lookupID: "netscaler.service.state",
			expected: "prtg.valuelookup.netscaler_service_state.ovl",
		},
		{
			name:     "Simple lookup ID",
			lookupID: "simple",
			expected: "prtg.valuelookup.simple.ovl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := generator.GetFilenameForLookup(tt.lookupID)
			if filename != tt.expected {
				t.Errorf("Expected filename '%s', got '%s'", tt.expected, filename)
			}
		})
	}
}

// TestSeverityToPRTGState tests severity to PRTG state conversion
func TestSeverityToPRTGState(t *testing.T) {
	tests := []struct {
		name     string
		severity string
		expected string
	}{
		{"OK severity", "ok", "Ok"},
		{"Warning severity", "warning", "Warning"},
		{"Error severity", "error", "Error"},
		{"Unknown severity", "unknown", "Unknown"},
		{"OK uppercase", "OK", "Ok"},
		{"WARNING uppercase", "WARNING", "Warning"},
		{"ERROR uppercase", "ERROR", "Error"},
		{"Invalid severity", "invalid", "Unknown"},
		{"Empty severity", "", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := severityToPRTGState(tt.severity)
			if result != tt.expected {
				t.Errorf("severityToPRTGState(%q) = %q, expected %q", tt.severity, result, tt.expected)
			}
		})
	}
}

// TestValidateLookupForPRTG tests PRTG validation
func TestValidateLookupForPRTG(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	// Test validating existing lookup
	err = generator.ValidateLookupForPRTG("netscaler.lbvserver.state")
	if err != nil {
		t.Errorf("Validation should pass for netscaler.lbvserver.state: %v", err)
	}

	// Test validating non-existent lookup
	err = generator.ValidateLookupForPRTG("nonexistent.lookup")
	if err == nil {
		t.Error("Expected error for non-existent lookup")
	}
}

// TestPRTGXMLStructure tests the PRTG XML structure is correct
func TestPRTGXMLStructure(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	xmlContent, err := generator.GenerateXML("netscaler.lbvserver.state")
	if err != nil {
		t.Fatalf("GenerateXML failed: %v", err)
	}

	// Remove XML header
	xmlContent = strings.TrimPrefix(xmlContent, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")

	var prtgLookup PRTGValueLookup
	if err := xml.Unmarshal([]byte(xmlContent), &prtgLookup); err != nil {
		t.Fatalf("Failed to unmarshal XML: %v", err)
	}

	// Verify structure
	if prtgLookup.ID == "" {
		t.Error("PRTG lookup should have ID")
	}

	if prtgLookup.DesiredValue == 0 {
		t.Error("PRTG lookup should have desiredValue")
	}

	if len(prtgLookup.Lookups.Entries) == 0 {
		t.Error("PRTG lookup should have entries")
	}

	// Verify each entry has required fields
	for i, entry := range prtgLookup.Lookups.Entries {
		if entry.State == "" {
			t.Errorf("Entry %d should have state", i)
		}

		if entry.Text == "" {
			t.Errorf("Entry %d should have text", i)
		}

		// Verify state is one of the valid PRTG states
		validStates := map[string]bool{
			"Ok":      true,
			"Warning": true,
			"Error":   true,
			"Unknown": true,
		}

		if !validStates[entry.State] {
			t.Errorf("Entry %d has invalid state '%s'", i, entry.State)
		}
	}
}

// TestPRTGXMLSortedByValue tests that XML entries are sorted by value
func TestPRTGXMLSortedByValue(t *testing.T) {
	baseLogger := zerolog.New(nil)
	registry, err := NewLookupRegistry(&baseLogger)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	generator := NewPRTGLookupGenerator(registry)

	xmlContent, err := generator.GenerateXML("netscaler.lbvserver.state")
	if err != nil {
		t.Fatalf("GenerateXML failed: %v", err)
	}

	// Remove XML header
	xmlContent = strings.TrimPrefix(xmlContent, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")

	var prtgLookup PRTGValueLookup
	if err := xml.Unmarshal([]byte(xmlContent), &prtgLookup); err != nil {
		t.Fatalf("Failed to unmarshal XML: %v", err)
	}

	// Verify entries are sorted by value
	for i := 1; i < len(prtgLookup.Lookups.Entries); i++ {
		prev := prtgLookup.Lookups.Entries[i-1].Value
		curr := prtgLookup.Lookups.Entries[i].Value

		if prev > curr {
			t.Errorf("Entries should be sorted by value: entry %d (value=%d) > entry %d (value=%d)",
				i-1, prev, i, curr)
		}
	}
}

// BenchmarkGenerateXML benchmarks XML generation for a single lookup
func BenchmarkGenerateXML(b *testing.B) {
	baseLogger := zerolog.New(nil)
	registry, _ := NewLookupRegistry(&baseLogger)
	generator := NewPRTGLookupGenerator(registry)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = generator.GenerateXML("netscaler.lbvserver.state")
	}
}

// BenchmarkGenerateAllXML benchmarks XML generation for all lookups
func BenchmarkGenerateAllXML(b *testing.B) {
	baseLogger := zerolog.New(nil)
	registry, _ := NewLookupRegistry(&baseLogger)
	generator := NewPRTGLookupGenerator(registry)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = generator.GenerateAllXML()
	}
}
