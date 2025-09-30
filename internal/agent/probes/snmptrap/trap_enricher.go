package snmptrap

import (
	"fmt"
	"regexp"
	"strings"

	"senhub-agent.go/internal/agent/services/logger"
)

// TrapEnricher enriches SNMP traps with MIB information
type TrapEnricher struct {
	mibManager       *MIBManager
	enterprises      map[string]EnterpriseInfo
	severityPatterns map[*regexp.Regexp]string
	logger           *logger.ModuleLogger
}

// NewTrapEnricher creates a new trap enricher
func NewTrapEnricher(mibManager *MIBManager, enterprises map[string]EnterpriseInfo, baseLogger *logger.Logger) *TrapEnricher {
	// Create module-specific logger for trap enricher
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.snmptrap.enricher")
	
	te := &TrapEnricher{
		mibManager:  mibManager,
		enterprises: enterprises,
		logger:      moduleLogger,
	}
	
	// Initialize severity patterns
	te.initSeverityPatterns()
	
	return te
}

// initSeverityPatterns initializes regex patterns for severity inference
func (te *TrapEnricher) initSeverityPatterns() {
	te.severityPatterns = map[*regexp.Regexp]string{
		regexp.MustCompile(`(?i)(critical|fatal|emergency|down|failed|error)`):      "critical",
		regexp.MustCompile(`(?i)(major|alarm|high|warning|threshold|exceed)`):       "warning",
		regexp.MustCompile(`(?i)(minor|info|up|ok|normal|clear|restored|start)`):    "info",
		regexp.MustCompile(`(?i)(debug|trace|test)`):                                "debug",
	}
}

// Enrich enriches a parsed trap with MIB information
func (te *TrapEnricher) Enrich(trap *ParsedTrap) *EnrichedTrap {
	enriched := &EnrichedTrap{
		Timestamp:  trap.Timestamp,
		SourceHost: trap.SourceIP,
		TrapOID:    trap.TrapOID,
		Severity:   "info", // Default severity
		Varbinds:   make(map[string]interface{}),
		Analysis:   make(map[string]interface{}),
	}
	
	te.logger.Debug().
		Str("source", trap.SourceIP).
		Str("trap_oid", trap.TrapOID).
		Msg("Enriching SNMP trap")
	
	// Resolve trap OID
	if trap.TrapOID != "" {
		te.logger.Debug().
			Str("trap_oid", trap.TrapOID).
			Msg("Attempting to resolve OID")
			
		if resolved := te.mibManager.ResolveOID(trap.TrapOID); resolved != nil {
			te.logger.Debug().
				Str("trap_oid", trap.TrapOID).
				Str("resolved_name", resolved.Name).
				Str("description", resolved.Description).
				Msg("Successfully resolved OID")
				
			enriched.TrapName = resolved.Name
			enriched.Description = resolved.Description
			
			// Infer severity from trap name
			if severity := te.inferSeverityFromName(resolved.Name); severity != "" {
				enriched.Severity = severity
			}
		} else {
			te.logger.Debug().
				Str("trap_oid", trap.TrapOID).
				Msg("Failed to resolve OID - not found in MIBs")
		}
	}
	
	// Extract and enrich enterprise information
	te.enrichEnterpriseInfo(enriched, trap.EnterpriseOID)
	
	// Enrich varbinds
	te.enrichVarbinds(enriched, trap.Varbinds)
	
	// Generate contextual message
	enriched.Message = te.generateMessage(enriched, trap)
	
	// Perform analysis
	te.analyzeTraP(enriched, trap)
	
	// Add raw data for debugging
	enriched.RawData = map[string]interface{}{
		"version":       getVersionString(trap.Version),
		"community":     trap.Community,
		"generic_trap":  trap.GenericTrap,
		"specific_trap": trap.SpecificTrap,
		"enterprise":    trap.EnterpriseOID,
	}
	
	te.logger.Debug().
		Str("trap_name", enriched.TrapName).
		Str("enterprise", enriched.Enterprise).
		Str("severity", enriched.Severity).
		Int("varbind_count", len(enriched.Varbinds)).
		Msg("Trap enrichment completed")
	
	return enriched
}

// BasicEnrich provides basic enrichment without MIB resolution
func (te *TrapEnricher) BasicEnrich(trap *ParsedTrap) *EnrichedTrap {
	enriched := &EnrichedTrap{
		Timestamp:  trap.Timestamp,
		SourceHost: trap.SourceIP,
		TrapOID:    trap.TrapOID,
		TrapName:   trap.TrapOID, // Use OID as name
		Severity:   "info",
		Varbinds:   make(map[string]interface{}),
		Analysis:   make(map[string]interface{}),
	}
	
	// Extract enterprise information
	te.enrichEnterpriseInfo(enriched, trap.EnterpriseOID)
	
	// Basic varbind processing
	te.basicVarbindProcessing(enriched, trap.Varbinds)
	
	// Generate basic message
	enriched.Message = te.generateBasicMessage(enriched, trap)
	
	// Add raw data
	enriched.RawData = map[string]interface{}{
		"version":       getVersionString(trap.Version),
		"community":     trap.Community,
		"generic_trap":  trap.GenericTrap,
		"specific_trap": trap.SpecificTrap,
		"enterprise":    trap.EnterpriseOID,
	}
	
	return enriched
}

// enrichEnterpriseInfo enriches trap with enterprise/vendor information
func (te *TrapEnricher) enrichEnterpriseInfo(enriched *EnrichedTrap, enterpriseOID string) {
	if enterpriseOID == "" {
		return
	}
	
	// Get enterprise info from OID
	if enterprise := GetEnterpriseFromOID(enterpriseOID); enterprise != nil {
		enriched.Enterprise = enterprise.Name
		enriched.EnterpriseFull = enterprise.FullName
		enriched.Category = enterprise.Category
		
		// Adjust severity based on category
		te.adjustSeverityByCategory(enriched, enterprise.Category)
		
		te.logger.Debug().
			Str("enterprise_oid", enterpriseOID).
			Str("enterprise_name", enterprise.Name).
			Str("category", enterprise.Category).
			Msg("Enterprise information enriched")
	}
}

// enrichVarbinds enriches variable bindings with MIB information
func (te *TrapEnricher) enrichVarbinds(enriched *EnrichedTrap, varbinds []Varbind) {
	for _, vb := range varbinds {
		enrichedVB := map[string]interface{}{
			"oid":   vb.OID,
			"type":  vb.Type,
			"value": formatValue(vb.Value),
		}
		
		// Resolve OID to name
		if resolved := te.mibManager.ResolveOID(vb.OID); resolved != nil {
			enrichedVB["name"] = resolved.Name
			enrichedVB["description"] = resolved.Description
			if resolved.Unit != "" {
				enrichedVB["unit"] = resolved.Unit
			}
			if resolved.Type != "" {
				enrichedVB["type"] = resolved.Type
			}
			
			// Generate human-readable representation
			enrichedVB["human_readable"] = te.formatHumanReadable(resolved, vb.Value)
		}
		
		// Use OID as key, but also try to use resolved name
		key := vb.OID
		if name, ok := enrichedVB["name"].(string); ok && name != "" {
			key = name
		}
		
		enriched.Varbinds[key] = enrichedVB
	}
}

// basicVarbindProcessing provides basic varbind processing without MIB resolution
func (te *TrapEnricher) basicVarbindProcessing(enriched *EnrichedTrap, varbinds []Varbind) {
	for _, vb := range varbinds {
		enriched.Varbinds[vb.OID] = map[string]interface{}{
			"oid":   vb.OID,
			"type":  vb.Type,
			"value": formatValue(vb.Value),
		}
	}
}

// generateMessage generates a contextual message for the trap
func (te *TrapEnricher) generateMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	// Try vendor-specific message generation
	if enriched.Enterprise != "" {
		if msg := te.generateVendorMessage(enriched, trap); msg != "" {
			return msg
		}
	}
	
	// Generate generic message
	return te.generateGenericMessage(enriched, trap)
}

// generateBasicMessage generates a basic message without MIB information
func (te *TrapEnricher) generateBasicMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	if enriched.EnterpriseFull != "" {
		return fmt.Sprintf("%s device alert from %s: SNMP trap %s",
			enriched.EnterpriseFull,
			enriched.SourceHost,
			enriched.TrapOID)
	}
	
	return fmt.Sprintf("SNMP trap received from %s: %s",
		enriched.SourceHost,
		enriched.TrapOID)
}

// generateVendorMessage generates vendor-specific messages
func (te *TrapEnricher) generateVendorMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	switch enriched.Enterprise {
	case "cisco":
		return te.generateCiscoMessage(enriched, trap)
	case "paloalto":
		return te.generatePaloAltoMessage(enriched, trap)
	case "fortinet":
		return te.generateFortinetMessage(enriched, trap)
	case "huawei":
		return te.generateHuaweiMessage(enriched, trap)
	default:
		return ""
	}
}

// generateCiscoMessage generates Cisco-specific messages
func (te *TrapEnricher) generateCiscoMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	if strings.Contains(enriched.TrapName, "Temperature") {
		if desc := te.getVarbindValue(enriched, "ciscoEnvMonTemperatureStatusDescr"); desc != "" {
			if value := te.getVarbindValue(enriched, "ciscoEnvMonTemperatureStatusValue"); value != "" {
				if threshold := te.getVarbindValue(enriched, "ciscoEnvMonTemperatureThreshold"); threshold != "" {
					return fmt.Sprintf("🌡️ Cisco Environmental Alert: %s reports %s°C (threshold: %s°C)",
						desc, value, threshold)
				}
				return fmt.Sprintf("🌡️ Cisco Environmental Alert: %s reports %s°C",
					desc, value)
			}
		}
		return "🌡️ Cisco temperature monitoring alert from " + enriched.SourceHost
	}
	
	if strings.Contains(enriched.TrapName, "linkDown") {
		return "🔴 Cisco Link Down: Interface down on " + enriched.SourceHost
	}
	
	if strings.Contains(enriched.TrapName, "linkUp") {
		return "🟢 Cisco Link Up: Interface up on " + enriched.SourceHost
	}
	
	return fmt.Sprintf("Cisco %s alert from %s", enriched.TrapName, enriched.SourceHost)
}

// generatePaloAltoMessage generates Palo Alto-specific messages  
func (te *TrapEnricher) generatePaloAltoMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	return fmt.Sprintf("🛡️ Palo Alto Networks alert from %s: %s",
		enriched.SourceHost, enriched.Description)
}

// generateFortinetMessage generates Fortinet-specific messages
func (te *TrapEnricher) generateFortinetMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	return fmt.Sprintf("🛡️ Fortinet security alert from %s: %s",
		enriched.SourceHost, enriched.Description)
}

// generateHuaweiMessage generates Huawei-specific messages
func (te *TrapEnricher) generateHuaweiMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	return fmt.Sprintf("⚠️ Huawei equipment alert from %s: %s",
		enriched.SourceHost, enriched.Description)
}

// generateGenericMessage generates a generic message
func (te *TrapEnricher) generateGenericMessage(enriched *EnrichedTrap, trap *ParsedTrap) string {
	if enriched.Description != "" && enriched.EnterpriseFull != "" {
		return fmt.Sprintf("%s alert from %s: %s",
			enriched.EnterpriseFull, enriched.SourceHost, enriched.Description)
	}
	
	if enriched.TrapName != "" && enriched.TrapName != enriched.TrapOID {
		return fmt.Sprintf("SNMP trap from %s: %s", enriched.SourceHost, enriched.TrapName)
	}
	
	return fmt.Sprintf("SNMP trap received from %s: %s", enriched.SourceHost, enriched.TrapOID)
}

// getVarbindValue gets a varbind value by name or OID
func (te *TrapEnricher) getVarbindValue(enriched *EnrichedTrap, nameOrOID string) string {
	if vb, exists := enriched.Varbinds[nameOrOID]; exists {
		if vbMap, ok := vb.(map[string]interface{}); ok {
			if value, ok := vbMap["value"]; ok {
				return fmt.Sprintf("%v", value)
			}
		}
	}
	return ""
}

// formatHumanReadable formats a value in human-readable form
func (te *TrapEnricher) formatHumanReadable(resolved *ResolvedOID, value interface{}) string {
	valueStr := formatValue(value)
	
	if resolved.Unit != "" {
		return fmt.Sprintf("%s: %s %s", resolved.Name, valueStr, resolved.Unit)
	}
	
	if resolved.Description != "" {
		return fmt.Sprintf("%s: %s", resolved.Description, valueStr)
	}
	
	return fmt.Sprintf("%s: %s", resolved.Name, valueStr)
}

// inferSeverityFromName infers severity from trap name
func (te *TrapEnricher) inferSeverityFromName(trapName string) string {
	for pattern, severity := range te.severityPatterns {
		if pattern.MatchString(trapName) {
			return severity
		}
	}
	return ""
}

// adjustSeverityByCategory adjusts severity based on equipment category
func (te *TrapEnricher) adjustSeverityByCategory(enriched *EnrichedTrap, category string) {
	// Security devices have higher priority
	if category == "security" && enriched.Severity == "info" {
		enriched.Severity = "warning"
	}
	
	// Critical infrastructure gets higher priority
	if category == "network" && enriched.Severity == "info" {
		enriched.Severity = "warning"
	}
}

// analyzeTraP performs additional analysis on the trap
func (te *TrapEnricher) analyzeTraP(enriched *EnrichedTrap, trap *ParsedTrap) {
	analysis := enriched.Analysis
	
	// Basic analysis
	analysis["varbind_count"] = len(trap.Varbinds)
	analysis["has_description"] = enriched.Description != ""
	analysis["vendor_identified"] = enriched.Enterprise != ""
	
	// Category-specific analysis
	if enriched.Category != "" {
		analysis["equipment_category"] = enriched.Category
		analysis["category_priority"] = GetCategoryPriority(enriched.Category)
	}
	
	// Pattern-based analysis
	if strings.Contains(strings.ToLower(enriched.TrapName), "temperature") {
		te.analyzeTemperature(enriched, analysis)
	} else if strings.Contains(strings.ToLower(enriched.TrapName), "link") {
		te.analyzeLink(enriched, analysis)
	}
}

// analyzeTemperature analyzes temperature-related traps
func (te *TrapEnricher) analyzeTemperature(enriched *EnrichedTrap, analysis map[string]interface{}) {
	analysis["alert_type"] = "temperature"
	
	// Look for temperature values and thresholds
	for _, vb := range enriched.Varbinds {
		if vbMap, ok := vb.(map[string]interface{}); ok {
			if name, ok := vbMap["name"].(string); ok {
				if strings.Contains(strings.ToLower(name), "temperature") && strings.Contains(strings.ToLower(name), "value") {
					analysis["current_temperature"] = vbMap["value"]
				} else if strings.Contains(strings.ToLower(name), "threshold") {
					analysis["temperature_threshold"] = vbMap["value"]
				}
			}
		}
	}
}

// analyzeLink analyzes link-related traps
func (te *TrapEnricher) analyzeLink(enriched *EnrichedTrap, analysis map[string]interface{}) {
	if strings.Contains(strings.ToLower(enriched.TrapName), "down") {
		analysis["alert_type"] = "link_down"
		analysis["link_state"] = "down"
	} else if strings.Contains(strings.ToLower(enriched.TrapName), "up") {
		analysis["alert_type"] = "link_up"
		analysis["link_state"] = "up"
	} else {
		analysis["alert_type"] = "link_change"
	}
}