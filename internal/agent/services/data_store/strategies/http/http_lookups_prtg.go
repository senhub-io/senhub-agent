// senhub-agent/internal/agent/services/data_store/strategies/http/http_lookups_prtg.go

package http

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// PRTGValueLookup represents a PRTG XML ValueLookup structure
type PRTGValueLookup struct {
	XMLName      xml.Name         `xml:"ValueLookup"`
	ID           string           `xml:"id,attr"`
	DesiredValue int              `xml:"desiredValue,attr"`
	Lookups      PRTGLookupValues `xml:"Lookups"`
}

// PRTGLookupValues contains the list of SingleInt entries
type PRTGLookupValues struct {
	Entries []PRTGSingleInt `xml:"SingleInt"`
}

// PRTGSingleInt represents a single lookup entry in PRTG format
type PRTGSingleInt struct {
	State string `xml:"state,attr"`
	Value int    `xml:"value,attr"`
	Text  string `xml:",chardata"`
}

// PRTGLookupGenerator generates PRTG XML lookup files
type PRTGLookupGenerator struct {
	registry *LookupRegistry
}

// NewPRTGLookupGenerator creates a new PRTG lookup generator
func NewPRTGLookupGenerator(registry *LookupRegistry) *PRTGLookupGenerator {
	return &PRTGLookupGenerator{
		registry: registry,
	}
}

// GenerateXML generates PRTG XML for a specific lookup
func (g *PRTGLookupGenerator) GenerateXML(lookupID string) (string, error) {
	// Get lookup definition
	lookup, exists := g.registry.GetLookup(lookupID)
	if !exists {
		return "", fmt.Errorf("lookup not found: %s", lookupID)
	}

	// Create PRTG structure
	prtgLookup := PRTGValueLookup{
		ID:           lookupID,
		DesiredValue: lookup.DesiredValue,
		Lookups: PRTGLookupValues{
			Entries: make([]PRTGSingleInt, 0, len(lookup.Mappings)),
		},
	}

	// Convert mappings to PRTG format
	// Sort by value for consistent output
	values := make([]int, 0, len(lookup.Mappings))
	for value := range lookup.Mappings {
		values = append(values, value)
	}
	sort.Ints(values)

	for _, value := range values {
		mapping := lookup.Mappings[value]
		entry := PRTGSingleInt{
			State: severityToPRTGState(mapping.Severity),
			Value: value,
			Text:  capitalizeLookupText(mapping.Text),
		}
		prtgLookup.Lookups.Entries = append(prtgLookup.Lookups.Entries, entry)
	}

	// Marshal to XML
	output, err := xml.MarshalIndent(prtgLookup, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal XML: %w", err)
	}

	// Add XML header
	xmlHeader := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	return xmlHeader + string(output), nil
}

// GenerateAllXML generates PRTG XML for all lookups
func (g *PRTGLookupGenerator) GenerateAllXML() (map[string]string, error) {
	allLookups := g.registry.GetAllLookups()
	result := make(map[string]string, len(allLookups))

	for id := range allLookups {
		xml, err := g.GenerateXML(id)
		if err != nil {
			return nil, fmt.Errorf("failed to generate XML for %s: %w", id, err)
		}
		result[id] = xml
	}

	return result, nil
}

// GenerateXMLForProbe generates PRTG XML for all lookups of a specific probe
func (g *PRTGLookupGenerator) GenerateXMLForProbe(probeType string) (map[string]string, error) {
	probeLookups := g.registry.GetLookupsForProbe(probeType)
	result := make(map[string]string, len(probeLookups))

	for id := range probeLookups {
		xml, err := g.GenerateXML(id)
		if err != nil {
			return nil, fmt.Errorf("failed to generate XML for %s: %w", id, err)
		}
		result[id] = xml
	}

	return result, nil
}

// GetFilenameForLookup returns the PRTG-compatible filename for a lookup
// PRTG requires the filename (without .ovl extension) to match exactly the
// ValueLookup id attribute in the XML. The file must be placed in:
// C:\Program Files (x86)\PRTG Network Monitor\lookups\custom\
// Example: netscaler.ha.node.state → netscaler.ha.node.state.ovl
func (g *PRTGLookupGenerator) GetFilenameForLookup(lookupID string) string {
	return fmt.Sprintf("%s.ovl", lookupID)
}

// severityToPRTGState converts severity levels to PRTG state names
func severityToPRTGState(severity string) string {
	switch strings.ToLower(severity) {
	case "ok":
		return "Ok"
	case "warning":
		return "Warning"
	case "error":
		return "Error"
	case "unknown":
		return "Unknown"
	default:
		return "Unknown"
	}
}

// capitalizeLookupText capitalizes lookup text for PRTG display
// Converts "UP" to "Up", "DOWN" to "Down", etc.
func capitalizeLookupText(text string) string {
	if text == "" {
		return text
	}

	// Convert to lowercase first
	lower := strings.ToLower(text)

	// Capitalize first letter
	return strings.ToUpper(string(lower[0])) + lower[1:]
}

// ValidateLookupForPRTG checks if a lookup is compatible with PRTG format
func (g *PRTGLookupGenerator) ValidateLookupForPRTG(lookupID string) error {
	lookup, exists := g.registry.GetLookup(lookupID)
	if !exists {
		return fmt.Errorf("lookup not found: %s", lookupID)
	}

	// Check if all mappings have valid severities
	validSeverities := map[string]bool{
		"ok":      true,
		"warning": true,
		"error":   true,
		"unknown": true,
	}

	for value, mapping := range lookup.Mappings {
		severity := strings.ToLower(mapping.Severity)
		if !validSeverities[severity] {
			return fmt.Errorf("invalid severity '%s' for value %d in lookup %s", mapping.Severity, value, lookupID)
		}
	}

	// Check if desired value exists in mappings
	if _, exists := lookup.Mappings[lookup.DesiredValue]; !exists {
		return fmt.Errorf("desired value %d not found in mappings for lookup %s", lookup.DesiredValue, lookupID)
	}

	return nil
}
