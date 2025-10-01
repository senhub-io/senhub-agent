package snmptrap

import (
	"time"

	"github.com/gosnmp/gosnmp"
)

// ParsedTrap represents a parsed SNMP trap
type ParsedTrap struct {
	Timestamp     time.Time
	SourceIP      string
	AgentAddress  string             // SNMPv1 agent address
	TrapOID       string
	EnterpriseOID string
	GenericTrap   int
	SpecificTrap  int
	Varbinds      []Varbind
	Version       gosnmp.SnmpVersion
	Community     string
}

// Varbind represents a single variable binding
type Varbind struct {
	OID   string
	Type  string
	Value interface{}
}

// EnrichedTrap represents a trap enriched with MIB information
type EnrichedTrap struct {
	// Basic information
	Timestamp    time.Time `json:"timestamp"`
	SourceHost   string    `json:"source_host"`
	AgentAddress string    `json:"agent_address,omitempty"` // SNMPv1 agent address
	TrapOID      string    `json:"trap_oid"`
	TrapName     string    `json:"trap_name"`

	// Enterprise information
	Enterprise     string `json:"enterprise"`
	EnterpriseFull string `json:"enterprise_full"`
	Category       string `json:"category"`

	// Message and severity
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Description string `json:"description"`

	// Enriched varbinds (detailed)
	Varbinds map[string]interface{} `json:"varbinds"`

	// Analysis
	Analysis map[string]interface{} `json:"analysis,omitempty"`

	// Raw data for debugging
	RawData interface{} `json:"raw_data,omitempty"`
}

// BufferStats represents buffer statistics
type BufferStats struct {
	CurrentSize int
	Capacity    int
	TotalAdded  int64
	TotalDropped int64
}

// MIBStats represents MIB manager statistics
type MIBStats struct {
	LoadedMIBCount     int       `json:"loaded_mibs"`
	CacheSize          int       `json:"cache_size"`
	CacheHitRate       float64   `json:"cache_hit_rate"`
	LastMIBLoadTime    time.Time `json:"last_mib_load"`
	OIDResolutionCount int64     `json:"oid_resolutions"`
	FailedResolutions  int64     `json:"failed_resolutions"`
}

// ResolvedOID represents an OID resolved from MIB
type ResolvedOID struct {
	OID         string
	Name        string
	Description string
	Unit        string
	Type        string
	Source      string // "embedded", "external", "numeric", "gosmi"
	Module      string // MIB module name (for gosmi)
}