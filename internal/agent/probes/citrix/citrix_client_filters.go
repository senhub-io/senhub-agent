package citrix

import (
	"strings"

	"senhub-agent.go/internal/agent/services/logger"
)

// ClientFilters provides DNS-based filtering functionality for Citrix data
type ClientFilters struct {
	validMachineDNS []string
	logger          *logger.ModuleLogger
}

// NewClientFilters creates a new client filters instance
func NewClientFilters(baseLogger *logger.Logger) *ClientFilters {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.filters")
	return &ClientFilters{
		logger: moduleLogger,
	}
}

// SetValidMachineDNS sets the list of valid machine DNS names for filtering
func (f *ClientFilters) SetValidMachineDNS(dnsNames []string) {
	f.validMachineDNS = dnsNames
	f.logger.Debug().
		Int("dns_count", len(dnsNames)).
		Msg("Updated valid machine DNS list for client-side filtering")
}

// GetValidMachineDNS returns the current list of valid machine DNS names
func (f *ClientFilters) GetValidMachineDNS() []string {
	return f.validMachineDNS
}

// FilterSessionsByMachineDNS filters sessions to only include those from specified machine DNS names
func (f *ClientFilters) FilterSessionsByMachineDNS(sessions []Session) []Session {
	if len(f.validMachineDNS) == 0 {
		// No filter, return all sessions
		return sessions
	}

	// Create a map for O(1) lookup with both DNS names and short names
	machineMap := make(map[string]bool)
	for _, dns := range f.validMachineDNS {
		if dns != "" {
			machineMap[strings.ToLower(dns)] = true
			// Also add short hostname (before first dot) for compatibility
			if dotIndex := strings.Index(dns, "."); dotIndex > 0 {
				shortName := dns[:dotIndex]
				machineMap[strings.ToLower(shortName)] = true
			}
		}
	}

	var filteredSessions []Session
	var debugCount int

	for _, session := range sessions {
		// RESTORATION: Use the EXACT logic from working version
		var machineDNS string

		// Get machine DNS from expanded Machine data
		if session.Machine != nil {
			machineDNS = session.Machine.DnsName
		}

		// Debug first few sessions to understand naming format
		if debugCount < 5 {
			f.logger.Debug().
				Str("session_key", session.SessionKey).
				Str("machine_dns", machineDNS).
				Str("session_user", session.UserName).
				Int("connection_state", session.ConnectionState).
				Msg("🔍 SIMPLE: Session filtering - only Machine.DnsName")
			debugCount++
		}

		// SINGLE RULE: Only use Machine.DnsName - NO FALLBACKS
		if session.Machine != nil && session.Machine.DnsName != "" {
			machineDns := strings.ToLower(session.Machine.DnsName)
			if machineMap[machineDns] {
				filteredSessions = append(filteredSessions, session)
				f.logger.Debug().
					Str("session_key", session.SessionKey).
					Str("machine_dns", session.Machine.DnsName).
					Msg("✅ Session INCLUDED by Machine.DnsName")
			} else {
				f.logger.Debug().
					Str("session_key", session.SessionKey).
					Str("machine_dns", session.Machine.DnsName).
					Msg("❌ Session EXCLUDED - Machine.DnsName not in CVAD inventory")
			}
		} else {
			// Session without Machine expansion or DNS - EXCLUDE
			f.logger.Debug().
				Str("session_key", session.SessionKey).
				Str("legacy_machine_name", session.MachineName).
				Msg("❌ Session EXCLUDED - No Machine.DnsName available")
		}
	}

	f.logger.Debug().
		Int("total_sessions", len(sessions)).
		Int("filtered_sessions", len(filteredSessions)).
		Int("valid_dns_count", len(f.validMachineDNS)).
		Msg("Applied DNS filtering to sessions")

	return filteredSessions
}

// FilterConnectionsByMachineDNS filters connections - currently returns all connections
// since Connection struct doesn't have a direct MachineName field
// TODO: Implement proper connection filtering based on SessionKey relationship with sessions
func (f *ClientFilters) FilterConnectionsByMachineDNS(connections []Connection) []Connection {
	// For now, return all connections until we implement SessionKey-based filtering
	f.logger.Debug().
		Int("total_connections", len(connections)).
		Msg("Connection filtering not yet implemented - returning all connections")

	return connections
}

// FilterFailureLogsByMachineDNS filters connection failure logs to only include those from specified machine DNS names
func (f *ClientFilters) FilterFailureLogsByMachineDNS(failures []ConnectionFailureLog) []ConnectionFailureLog {
	if len(f.validMachineDNS) == 0 {
		// No filter, return all failures
		return failures
	}

	// Create a map for O(1) lookup with both DNS names and short names
	machineMap := make(map[string]bool)
	for _, dns := range f.validMachineDNS {
		if dns != "" {
			machineMap[strings.ToLower(dns)] = true
			// Also add short hostname (before first dot) for compatibility
			if dotIndex := strings.Index(dns, "."); dotIndex > 0 {
				shortName := dns[:dotIndex]
				machineMap[strings.ToLower(shortName)] = true
			}
		}
	}

	var filteredFailures []ConnectionFailureLog
	for _, failure := range failures {
		machineName := failure.MachineName

		// Include failures without a machine name (global failures: licensing, client-side, etc.)
		if machineName == "" {
			filteredFailures = append(filteredFailures, failure)
			continue
		}

		lowerMachineName := strings.ToLower(machineName)

		// Handle DOMAIN\MACHINE format — extract machine name after backslash
		if backslash := strings.LastIndex(lowerMachineName, "\\"); backslash >= 0 {
			lowerMachineName = lowerMachineName[backslash+1:]
		}

		if machineMap[lowerMachineName] {
			filteredFailures = append(filteredFailures, failure)
			continue
		}

		// Also check short hostname
		if dotIndex := strings.Index(lowerMachineName, "."); dotIndex > 0 {
			shortName := lowerMachineName[:dotIndex]
			if machineMap[shortName] {
				filteredFailures = append(filteredFailures, failure)
			}
		}
	}

	f.logger.Debug().
		Int("total_failures", len(failures)).
		Int("filtered_failures", len(filteredFailures)).
		Int("valid_dns_count", len(f.validMachineDNS)).
		Msg("Applied DNS filtering to connection failure logs")

	return filteredFailures
}
