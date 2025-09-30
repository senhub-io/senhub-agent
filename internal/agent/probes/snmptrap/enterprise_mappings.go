package snmptrap

import (
	"strings"
)

// EnterpriseInfo contains information about a vendor/manufacturer
type EnterpriseInfo struct {
	Name     string // Short name for logs/display
	FullName string // Full official name
	Category string // Equipment category
}

// KnownEnterprises contains all known enterprise OIDs
// Format: Enterprise OID (without 1.3.6.1.4.1. prefix) → Vendor info
var KnownEnterprises = map[string]EnterpriseInfo{
	// NETWORK EQUIPMENT - GENERAL
	"9":     {Name: "cisco", FullName: "Cisco Systems", Category: "network"},
	"2636":  {Name: "juniper", FullName: "Juniper Networks", Category: "network"},
	"11":    {Name: "hp", FullName: "Hewlett-Packard", Category: "network"},
	"14823": {Name: "aruba", FullName: "Aruba Networks", Category: "network"},
	"30065": {Name: "arista", FullName: "Arista Networks", Category: "network"},
	"1916":  {Name: "extreme", FullName: "Extreme Networks", Category: "network"},
	"6027":  {Name: "force10", FullName: "Dell Force10", Category: "network"},
	"2011":  {Name: "huawei", FullName: "Huawei", Category: "network"},
	"3902":  {Name: "zte", FullName: "ZTE", Category: "network"},
	"6486":  {Name: "alcatel", FullName: "Alcatel-Lucent (Nokia)", Category: "network"},
	"637":   {Name: "nokia", FullName: "Nokia", Category: "network"},
	"1991":  {Name: "brocade", FullName: "Brocade", Category: "network"},
	"43":    {Name: "3com", FullName: "3Com", Category: "network"},
	"562":   {Name: "nortel", FullName: "Nortel", Category: "network"},
	"5624":  {Name: "enterasys", FullName: "Enterasys", Category: "network"},
	"6889":  {Name: "avaya", FullName: "Avaya", Category: "network"},
	"207":   {Name: "allied", FullName: "Allied Telesis", Category: "network"},
	"193":   {Name: "ericsson", FullName: "Ericsson", Category: "network"},
	"161":   {Name: "motorola", FullName: "Motorola", Category: "network"},
	
	// SMB/SOHO EQUIPMENT
	"4526":  {Name: "netgear", FullName: "Netgear", Category: "soho"},
	"171":   {Name: "dlink", FullName: "D-Link", Category: "soho"},
	"11863": {Name: "tplink", FullName: "TP-Link", Category: "soho"},
	"41112": {Name: "ubiquiti", FullName: "Ubiquiti Networks", Category: "soho"},
	"14988": {Name: "mikrotik", FullName: "MikroTik", Category: "soho"},
	
	// WIRELESS & ACCESS POINTS
	"25053": {Name: "ruckus", FullName: "Ruckus Wireless", Category: "wireless"},
	"26928": {Name: "aerohive", FullName: "Aerohive", Category: "wireless"},
	"17713": {Name: "cambium", FullName: "Cambium Networks", Category: "wireless"},
	"29671": {Name: "meraki", FullName: "Meraki (Cisco)", Category: "wireless"},
	
	// FIREWALLS & SECURITY
	"25461": {Name: "paloalto", FullName: "Palo Alto Networks", Category: "security"},
	"12356": {Name: "fortinet", FullName: "Fortinet", Category: "security"},
	"2620":  {Name: "checkpoint", FullName: "Check Point", Category: "security"},
	"21067": {Name: "sophos", FullName: "Sophos", Category: "security"},
	"8741":  {Name: "sonicwall", FullName: "SonicWall", Category: "security"},
	"3097":  {Name: "watchguard", FullName: "WatchGuard", Category: "security"},
	"20632": {Name: "barracuda", FullName: "Barracuda Networks", Category: "security"},
	"11256": {Name: "stormshield", FullName: "Stormshield", Category: "security"},
	"3417":  {Name: "bluecoat", FullName: "Blue Coat", Category: "security"},
	"12325": {Name: "pfsense", FullName: "pfSense (Netgate)", Category: "security"},
	"74":    {Name: "vyatta", FullName: "Vyatta/VyOS", Category: "security"},
	
	// LOAD BALANCERS & ADC
	"3375":  {Name: "f5", FullName: "F5 Networks", Category: "loadbalancer"},
	"5951":  {Name: "citrix", FullName: "Citrix (NetScaler)", Category: "loadbalancer"},
	"22610": {Name: "a10", FullName: "A10 Networks", Category: "loadbalancer"},
	"12196": {Name: "kemp", FullName: "Kemp Technologies", Category: "loadbalancer"},
	"89":    {Name: "radware", FullName: "Radware", Category: "loadbalancer"},
	"7564":  {Name: "array", FullName: "Array Networks", Category: "loadbalancer"},
	"29385": {Name: "haproxy", FullName: "HAProxy Technologies", Category: "loadbalancer"},
	"50757": {Name: "nginx", FullName: "NGINX", Category: "loadbalancer"},
	
	// WAN OPTIMIZATION & MONITORING
	"17163": {Name: "riverbed", FullName: "Riverbed", Category: "wan"},
	"23867": {Name: "silverpeak", FullName: "Silver Peak (HPE)", Category: "wan"},
	"9694":  {Name: "netscout", FullName: "Netscout (Arbor Networks)", Category: "monitoring"},
	
	// DATA CENTER & FABRIC
	"33049": {Name: "mellanox", FullName: "Mellanox", Category: "datacenter"},
	"26866": {Name: "gigamon", FullName: "Gigamon", Category: "datacenter"},
	"7779":  {Name: "infoblox", FullName: "Infoblox", Category: "datacenter"},
	
	// SERVERS & STORAGE
	"674": {Name: "dell", FullName: "Dell", Category: "server"},
	"232": {Name: "hpe", FullName: "HPE", Category: "server"},
	
	// SNMP AGENTS
	"8072": {Name: "netsnmp", FullName: "Net-SNMP", Category: "agent"},
	"2021": {Name: "ucdsnmp", FullName: "UCD-SNMP", Category: "agent"},
}

// GetEnterpriseFromOID returns enterprise information from a full OID
func GetEnterpriseFromOID(fullOID string) *EnterpriseInfo {
	// Extract enterprise ID from full OID
	// Example: 1.3.6.1.4.1.9.9.41.2.0.1 → 9 (Cisco)
	const prefix = "1.3.6.1.4.1."
	
	if !strings.HasPrefix(fullOID, prefix) {
		// Try without dots for numeric OID
		if strings.HasPrefix(fullOID, "13614") {
			fullOID = formatOID(fullOID)
		}
		if !strings.HasPrefix(fullOID, prefix) {
			return nil
		}
	}
	
	remaining := strings.TrimPrefix(fullOID, prefix)
	parts := strings.Split(remaining, ".")
	if len(parts) == 0 {
		return nil
	}
	
	enterpriseID := parts[0]
	if info, exists := KnownEnterprises[enterpriseID]; exists {
		infoCopy := info // Create a copy to avoid modifying the original
		return &infoCopy
	}
	
	// Unknown enterprise but valid OID
	return &EnterpriseInfo{
		Name:     "enterprise_" + enterpriseID,
		FullName: "Unknown Enterprise (" + enterpriseID + ")",
		Category: "unknown",
	}
}

// GetCategoryPriority returns the priority of a category for filtering/alerting
func GetCategoryPriority(category string) int {
	priorities := map[string]int{
		"security":     1,  // Highest priority
		"network":      2,
		"loadbalancer": 3,
		"server":       4,
		"datacenter":   5,
		"wireless":     6,
		"wan":          7,
		"monitoring":   8,
		"soho":         9,
		"agent":        10,
		"unknown":      99,
	}
	
	if priority, exists := priorities[category]; exists {
		return priority
	}
	return 99
}

// formatOID converts numeric OID without dots to dotted format
func formatOID(oid string) string {
	// Simple conversion for common cases
	// This is a simplified version, a full implementation would be more complex
	if len(oid) < 6 {
		return oid
	}
	
	// Try to format as dotted OID
	var result strings.Builder
	for i := 0; i < len(oid); i++ {
		if i > 0 {
			result.WriteByte('.')
		}
		result.WriteByte(oid[i])
	}
	return result.String()
}

// IsAllowedEnterprise checks if an enterprise OID is in the allowed list
func IsAllowedEnterprise(enterpriseOID string, allowedList []string) bool {
	if len(allowedList) == 0 {
		// No filter means all are allowed
		return true
	}
	
	for _, allowed := range allowedList {
		// Check for exact match first
		if enterpriseOID == allowed {
			return true
		}
		// Check for proper prefix match (must end with a dot to avoid partial matches)
		if strings.HasPrefix(enterpriseOID, allowed+".") {
			return true
		}
	}
	
	return false
}