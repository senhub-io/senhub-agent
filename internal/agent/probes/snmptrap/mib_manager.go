package snmptrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/types"
	"senhub-agent.go/internal/agent/probes/snmptrap/mibs"
	"senhub-agent.go/internal/agent/services/logger"
)

// MIBManager manages MIB loading and OID resolution
type MIBManager struct {
	config       MIBConfig
	cache        *MIBCache
	loadedMIBs   map[string]*MIBInfo
	enterprises  map[string]EnterpriseInfo
	mutex        sync.RWMutex
	logger       *logger.ModuleLogger
	
	// Statistics
	stats struct {
		loadedMIBCount     int
		cacheHitCount      int64
		cacheMissCount     int64
		resolutionCount    int64
		failedResolutions  int64
		lastLoadTime       time.Time
	}
}

// MIBInfo contains information about a loaded MIB
type MIBInfo struct {
	Name         string
	FilePath     string
	LoadTime     time.Time
	OIDMappings  map[string]string // OID → Name
	NameMappings map[string]string // Name → OID
	Descriptions map[string]string // OID → Description
	Types        map[string]string // OID → Type
	Units        map[string]string // OID → Unit
}

// NewMIBManager creates a new MIB manager
func NewMIBManager(config MIBConfig, baseLogger *logger.Logger) *MIBManager {
	// Create module-specific logger for MIB manager
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.snmptrap.mib")
	
	mm := &MIBManager{
		config:      config,
		loadedMIBs:  make(map[string]*MIBInfo),
		enterprises: KnownEnterprises,
		logger:      moduleLogger,
	}
	
	// Initialize cache
	mm.cache = NewMIBCache(config.CacheSize, baseLogger)
	
	return mm
}

// LoadMIBs loads MIBs from configured paths using gosmi
func (mm *MIBManager) LoadMIBs() error {
	mm.logger.Info().Msg("🚀 Starting LoadMIBs() function")
	mm.mutex.Lock()
	defer mm.mutex.Unlock()
	
	mm.logger.Info().
		Bool("enabled", mm.config.Enabled).
		Int("cache_size", mm.config.CacheSize).
		Msg("Loading MIBs for SNMP trap enrichment")
	
	if !mm.config.Enabled {
		mm.logger.Debug().Msg("MIB enrichment disabled, skipping MIB loading")
		return nil
	}
	
	// Initialize gosmi
	mm.logger.Debug().Msg("Initializing gosmi")
	gosmi.Init()
	
	// Load external MIBs if path is configured
	if mm.config.ExternalMIBsPath != "" {
		resolvedPath := mm.resolveExternalMIBsPath()
		if resolvedPath != "" {
			mm.logger.Debug().
				Str("configured_path", mm.config.ExternalMIBsPath).
				Str("resolved_path", resolvedPath).
				Msg("Setting MIB path for gosmi")
			
			// Set MIB path for gosmi
			gosmi.SetPath(resolvedPath)
			
			// Load standard MIBs first
			standardMIBs := []string{
				"SNMPv2-SMI",
				"SNMPv2-TC", 
				"SNMPv2-MIB",
				"IF-MIB",
				"HOST-RESOURCES-MIB",
			}
			
			loadedCount := 0
			for _, mibName := range standardMIBs {
				if _, err := gosmi.LoadModule(mibName); err != nil {
					mm.logger.Debug().
						Err(err).
						Str("mib", mibName).
						Msg("Failed to load standard MIB (may not exist)")
				} else {
					loadedCount++
					mm.logger.Debug().
						Str("mib", mibName).
						Msg("Loaded standard MIB")
				}
			}
			
			mm.logger.Info().
				Int("standard_mibs", loadedCount).
				Msg("✅ Standard MIBs loaded successfully")
			
			// Try to load vendor MIBs from subdirectories
			vendorCount := mm.loadVendorMIBs(resolvedPath)
			
			mm.logger.Info().
				Str("path", resolvedPath).
				Int("scanned_dirs", mm.countDirectories(resolvedPath)).
				Int("scanned_files", mm.countMIBFiles(resolvedPath)).
				Int("loaded_count", loadedCount+vendorCount).
				Msg("External MIBs scan completed")
			
			mm.stats.loadedMIBCount = loadedCount + vendorCount
		} else {
			mm.logger.Warn().
				Str("path", mm.config.ExternalMIBsPath).
				Msg("Could not resolve external MIBs path")
		}
	}
	
	mm.stats.lastLoadTime = time.Now()
	
	mm.logger.Info().
		Int("loaded_mibs", mm.stats.loadedMIBCount).
		Msg("MIB loading completed")
	
	return nil
}

// loadVendorMIBs loads vendor-specific MIBs from subdirectories 
func (mm *MIBManager) loadVendorMIBs(basePath string) int {
	loadedCount := 0
	
	// Walk through subdirectories and try to load MIBs
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue on errors
		}
		
		// Skip if it's a directory
		if info.IsDir() {
			return nil
		}
		
		// Only process files without extensions or with common MIB extensions
		ext := filepath.Ext(path)
		if ext != "" && ext != ".mib" && ext != ".txt" {
			return nil
		}
		
		// Extract module name from filename
		fileName := filepath.Base(path)
		moduleName := strings.TrimSuffix(fileName, ext)
		
		// Skip if already loaded
		if gosmi.IsLoaded(moduleName) {
			return nil
		}
		
		// Try to load the module
		if _, err := gosmi.LoadModule(moduleName); err != nil {
			mm.logger.Debug().
				Err(err).
				Str("file", path).
				Str("module", moduleName).
				Msg("Failed to load vendor MIB")
		} else {
			loadedCount++
			mm.logger.Debug().
				Str("module", moduleName).
				Str("file", path).
				Msg("Loaded vendor MIB")
		}
		
		return nil
	})
	
	return loadedCount
}

// countDirectories counts subdirectories in the MIB path
func (mm *MIBManager) countDirectories(basePath string) int {
	count := 0
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() && path != basePath {
			count++
		}
		return nil
	})
	return count
}

// countMIBFiles counts potential MIB files in the path
func (mm *MIBManager) countMIBFiles(basePath string) int {
	count := 0
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			ext := filepath.Ext(path)
			if ext == "" || ext == ".mib" || ext == ".txt" {
				count++
			}
		}
		return nil
	})
	return count
}

// parseMIBContent parses MIB content and extracts OID mappings
func (mm *MIBManager) parseMIBContent(content string, mibInfo *MIBInfo) {
	// Simple regex-based parsing for NOTIFICATION-TYPE and OBJECT-TYPE
	lines := strings.Split(content, "\n")
	var currentName string
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Match NOTIFICATION-TYPE or trap definitions
		if strings.Contains(line, "NOTIFICATION-TYPE") || strings.Contains(line, "TRAP-TYPE") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				currentName = parts[0]
			}
		}
		
		// Match OID assignments (::= { ... })
		if strings.Contains(line, "::=") && strings.Contains(line, "{") {
			// Extract OID from line like: ::= { cisco 9 41 2 0 1 }
			start := strings.Index(line, "{")
			end := strings.Index(line, "}")
			if start > 0 && end > start {
				oidParts := strings.Fields(line[start+1:end])
				if len(oidParts) > 0 {
					// Convert to dotted notation
					oid := mm.buildOIDFromParts(oidParts)
					if currentName != "" && oid != "" {
						mibInfo.OIDMappings[oid] = currentName
						currentName = ""
					}
				}
			}
		}
		
		// Match DESCRIPTION
		if strings.Contains(line, "DESCRIPTION") {
			// Capture description on next lines
			// Simple implementation - could be enhanced
		}
	}
}

// resolveExternalMIBsPath resolves relative paths to absolute ones
func (mm *MIBManager) resolveExternalMIBsPath() string {
	configuredPath := mm.config.ExternalMIBsPath
	
	mm.logger.Debug().
		Str("configured_path", configuredPath).
		Msg("Starting MIBs path resolution")
	
	// If already absolute, return as-is
	if filepath.IsAbs(configuredPath) {
		mm.logger.Debug().
			Str("path", configuredPath).
			Msg("Path is absolute, checking if exists")
		if _, err := os.Stat(configuredPath); err == nil {
			mm.logger.Debug().
				Str("path", configuredPath).
				Msg("Absolute path exists")
			return configuredPath
		}
		mm.logger.Debug().
			Str("path", configuredPath).
			Msg("Absolute path does not exist")
		return ""
	}
	
	// Handle special relative paths
	switch configuredPath {
	case "./mibs", "mibs":
		mm.logger.Debug().
			Str("path", configuredPath).
			Msg("Handling special relative path")
			
		// Try relative to executable directory
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			mibPath := filepath.Join(execDir, "mibs")
			mm.logger.Debug().
				Str("executable", execPath).
				Str("exec_dir", execDir).
				Str("checking_path", mibPath).
				Msg("Checking MIBs path relative to executable")
			if _, err := os.Stat(mibPath); err == nil {
				mm.logger.Debug().
					Str("executable", execPath).
					Str("mibs_path", mibPath).
					Msg("Found MIBs directory relative to executable")
				return mibPath
			} else {
				mm.logger.Debug().
					Err(err).
					Str("path", mibPath).
					Msg("MIBs path relative to executable does not exist")
			}
		} else {
			mm.logger.Debug().
				Err(err).
				Msg("Failed to get executable path")
		}
		
		// Try current working directory
		if wd, err := os.Getwd(); err == nil {
			mibPath := filepath.Join(wd, "mibs")
			mm.logger.Debug().
				Str("working_dir", wd).
				Str("checking_path", mibPath).
				Msg("Checking MIBs path relative to working directory")
			if _, err := os.Stat(mibPath); err == nil {
				mm.logger.Debug().
					Str("working_dir", wd).
					Str("mibs_path", mibPath).
					Msg("Found MIBs directory relative to working directory")
				return mibPath
			} else {
				mm.logger.Debug().
					Err(err).
					Str("path", mibPath).
					Msg("MIBs path relative to working directory does not exist")
			}
		} else {
			mm.logger.Debug().
				Err(err).
				Msg("Failed to get working directory")
		}
		
	default:
		mm.logger.Debug().
			Str("path", configuredPath).
			Msg("Handling custom relative path")
			
		// Try relative to current working directory
		if wd, err := os.Getwd(); err == nil {
			mibPath := filepath.Join(wd, configuredPath)
			mm.logger.Debug().
				Str("working_dir", wd).
				Str("checking_path", mibPath).
				Msg("Checking custom path relative to working directory")
			if _, err := os.Stat(mibPath); err == nil {
				mm.logger.Debug().
					Str("mibs_path", mibPath).
					Msg("Found MIBs directory relative to working directory")
				return mibPath
			}
		}
		
		// Try relative to executable directory
		if execPath, err := os.Executable(); err == nil {
			execDir := filepath.Dir(execPath)
			mibPath := filepath.Join(execDir, configuredPath)
			mm.logger.Debug().
				Str("exec_dir", execDir).
				Str("checking_path", mibPath).
				Msg("Checking custom path relative to executable")
			if _, err := os.Stat(mibPath); err == nil {
				mm.logger.Debug().
					Str("mibs_path", mibPath).
					Msg("Found MIBs directory relative to executable")
				return mibPath
			}
		}
	}
	
	mm.logger.Warn().
		Str("configured_path", configuredPath).
		Msg("Could not resolve MIBs path - no valid directory found")
	return ""
}

// buildOIDFromParts converts OID parts to dotted notation
func (mm *MIBManager) buildOIDFromParts(parts []string) string {
	// Known prefixes
	prefixes := map[string]string{
		"cisco":     "1.3.6.1.4.1.9",
		"enterprises": "1.3.6.1.4.1",
		"snmpTraps": "1.3.6.1.6.3.1.1.5",
		"ifMIB":     "1.3.6.1.2.1.31",
	}
	
	var oidParts []string
	for _, part := range parts {
		if prefix, ok := prefixes[part]; ok {
			oidParts = append(oidParts, strings.Split(prefix, ".")...)
		} else if num, err := strconv.Atoi(part); err == nil {
			oidParts = append(oidParts, strconv.Itoa(num))
		}
	}
	
	if len(oidParts) > 0 {
		return strings.Join(oidParts, ".")
	}
	return ""
}


// loadEmbeddedMIBs loads essential MIBs that are embedded in the binary
func (mm *MIBManager) loadSimpleEmbeddedMIBs() {
	mm.logger.Info().Msg("🏗️ Loading embedded MIBs")
	
	// Load essential standard MIBs
	standardMIBs := map[string]*MIBInfo{
		"SNMPv2-SMI": {
			Name:     "SNMPv2-SMI",
			FilePath: "embedded:standard/SNMPv2-SMI.txt",
			LoadTime: time.Now(),
			OIDMappings: map[string]string{
				"1.3.6.1.6.3.1.1.5.1": "coldStart",
				"1.3.6.1.6.3.1.1.5.2": "warmStart", 
				"1.3.6.1.6.3.1.1.5.3": "linkDown",
				"1.3.6.1.6.3.1.1.5.4": "linkUp",
				"1.3.6.1.6.3.1.1.5.5": "authenticationFailure",
				"1.3.6.1.6.3.1.1.5.6": "egpNeighborLoss",
			},
			Descriptions: map[string]string{
				"1.3.6.1.6.3.1.1.5.1": "A coldStart trap signifies that the SNMP entity is reinitializing itself",
				"1.3.6.1.6.3.1.1.5.2": "A warmStart trap signifies that the SNMP entity is reinitializing itself",
				"1.3.6.1.6.3.1.1.5.3": "A linkDown trap signifies that the SNMP entity has detected a link failure",
				"1.3.6.1.6.3.1.1.5.4": "A linkUp trap signifies that the SNMP entity has detected a link recovery",
				"1.3.6.1.6.3.1.1.5.5": "An authenticationFailure trap signifies that the SNMP entity has received a protocol message that is not properly authenticated",
				"1.3.6.1.6.3.1.1.5.6": "An egpNeighborLoss trap signifies that an EGP neighbor has gone down",
			},
		},
		
		"IF-MIB": {
			Name:     "IF-MIB",
			FilePath: "embedded:standard/IF-MIB.txt",
			LoadTime: time.Now(),
			OIDMappings: map[string]string{
				"1.3.6.1.2.1.2.2.1.1":  "ifIndex",
				"1.3.6.1.2.1.2.2.1.2":  "ifDescr",
				"1.3.6.1.2.1.2.2.1.3":  "ifType",
				"1.3.6.1.2.1.2.2.1.4":  "ifMtu",
				"1.3.6.1.2.1.2.2.1.5":  "ifSpeed",
				"1.3.6.1.2.1.2.2.1.6":  "ifPhysAddress",
				"1.3.6.1.2.1.2.2.1.7":  "ifAdminStatus",
				"1.3.6.1.2.1.2.2.1.8":  "ifOperStatus",
			},
			Descriptions: map[string]string{
				"1.3.6.1.2.1.2.2.1.2": "A textual string containing information about the interface",
				"1.3.6.1.2.1.2.2.1.7": "The desired state of the interface",
				"1.3.6.1.2.1.2.2.1.8": "The current operational state of the interface",
			},
		},
	}
	
	// Load vendor-specific MIBs
	vendorMIBs := mm.getVendorMIBs()
	
	// Combine all MIBs
	for name, mib := range standardMIBs {
		mm.loadedMIBs[name] = mib
	}
	
	for name, mib := range vendorMIBs {
		mm.loadedMIBs[name] = mib
	}
	
	// Populate reverse mappings
	for _, mib := range mm.loadedMIBs {
		mib.NameMappings = make(map[string]string)
		for oid, name := range mib.OIDMappings {
			mib.NameMappings[name] = oid
		}
	}
	
	mm.logger.Info().
		Int("standard_mibs", len(standardMIBs)).
		Int("vendor_mibs", len(vendorMIBs)).
		Int("total_loaded", len(mm.loadedMIBs)).
		Msg("✅ Embedded MIBs loaded successfully")
}

// getVendorMIBs returns embedded vendor-specific MIBs
func (mm *MIBManager) getVendorMIBs() map[string]*MIBInfo {
	return map[string]*MIBInfo{
		"CISCO-SMI": {
			Name:     "CISCO-SMI",
			FilePath: "embedded:vendors/cisco/CISCO-SMI.txt",
			LoadTime: time.Now(),
			OIDMappings: map[string]string{
				"1.3.6.1.4.1.9.9.41.2.0.1": "ciscoEnvMonTemperatureNotification",
				"1.3.6.1.4.1.9.9.41.2.0.2": "ciscoEnvMonFanNotification",
				"1.3.6.1.4.1.9.9.41.2.0.3": "ciscoEnvMonRedundantSupplyNotification",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.2": "ciscoEnvMonTemperatureStatusDescr",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.3": "ciscoEnvMonTemperatureStatusValue",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.4": "ciscoEnvMonTemperatureThreshold",
			},
			Descriptions: map[string]string{
				"1.3.6.1.4.1.9.9.41.2.0.1": "Temperature threshold exceeded notification",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.2": "Textual description of the environmental monitor",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.3": "The current measurement of the environmental monitor",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.4": "The configured threshold for the environmental monitor",
			},
			Units: map[string]string{
				"1.3.6.1.4.1.9.9.41.1.2.1.1.3": "degrees Celsius",
				"1.3.6.1.4.1.9.9.41.1.2.1.1.4": "degrees Celsius",
			},
		},
		
		"PAN-COMMON-MIB": {
			Name:     "PAN-COMMON-MIB", 
			FilePath: "embedded:vendors/paloalto/PAN-COMMON-MIB.txt",
			LoadTime: time.Now(),
			OIDMappings: map[string]string{
				"1.3.6.1.4.1.25461.2.1.3.1": "panCommonEventLog",
				"1.3.6.1.4.1.25461.2.1.3.2": "panCommonEventTrap",
			},
			Descriptions: map[string]string{
				"1.3.6.1.4.1.25461.2.1.3.2": "Palo Alto Networks generic event trap",
			},
		},
		
		"FORTINET-CORE-MIB": {
			Name:     "FORTINET-CORE-MIB",
			FilePath: "embedded:vendors/fortinet/FORTINET-CORE-MIB.txt", 
			LoadTime: time.Now(),
			OIDMappings: map[string]string{
				"1.3.6.1.4.1.12356.101.6.0.1": "fgTrapCpuHighThreshold",
				"1.3.6.1.4.1.12356.101.6.0.2": "fgTrapMemLowThreshold",
			},
			Descriptions: map[string]string{
				"1.3.6.1.4.1.12356.101.6.0.1": "CPU usage exceeds threshold",
				"1.3.6.1.4.1.12356.101.6.0.2": "Memory usage below threshold",
			},
		},
		
		"HUAWEI-MIB": {
			Name:     "HUAWEI-MIB",
			FilePath: "embedded:vendors/huawei/HUAWEI-MIB.txt",
			LoadTime: time.Now(), 
			OIDMappings: map[string]string{
				"1.3.6.1.4.1.2011.5.25.129.2.1.1": "hwEntityExtTemperatureThresholdNotification",
				"1.3.6.1.4.1.2011.5.25.129.2.1.2": "hwEntityExtVoltageLowThresholdNotification",
			},
			Descriptions: map[string]string{
				"1.3.6.1.4.1.2011.5.25.129.2.1.1": "Entity temperature exceeded threshold",
				"1.3.6.1.4.1.2011.5.25.129.2.1.2": "Entity voltage below threshold",
			},
		},
	}
}

// ResolveOID resolves an OID to its name and additional information
func (mm *MIBManager) ResolveOID(oid string) *ResolvedOID {
	mm.mutex.RLock()
	defer mm.mutex.RUnlock()
	
	mm.stats.resolutionCount++
	
	mm.logger.Info().
		Str("oid", oid).
		Int("loaded_mibs", mm.stats.loadedMIBCount).
		Msg("🔍 Attempting to resolve OID")
	
	// Check cache first
	if resolved := mm.cache.Get(oid); resolved != nil {
		mm.stats.cacheHitCount++
		mm.logger.Info().
			Str("oid", oid).
			Str("name", resolved.Name).
			Msg("✅ OID found in cache")
		return resolved
	}
	
	mm.stats.cacheMissCount++
	
	// Use gosmi to resolve OID
	if mm.config.Enabled {
		// Remove leading dot if present for gosmi and convert to types.Oid
		cleanOID := strings.TrimPrefix(oid, ".")
		
		// Convert string OID to types.Oid
		if typeOid, err := types.OidFromString(cleanOID); err == nil {
			// Try to get node by OID using gosmi
			if node, err := gosmi.GetNodeByOID(typeOid); err == nil {
				module := node.GetModule()
				
				mm.logger.Info().
					Str("oid", oid).
					Str("name", node.Name).
					Str("module", module.Name).
					Msg("✅ OID resolved using gosmi")
					
				resolved := &ResolvedOID{
					OID:         oid,
					Name:        node.Name,
					Description: node.Description,
					Source:      "gosmi",
					Module:      module.Name,
				}
				
				// Add additional info if available
				if node.Kind != types.NodeUnknown {
					resolved.Type = node.Kind.String()
				}
				
				// Cache the result
				mm.cache.Set(oid, resolved)
				
				return resolved
			}
		}
		
		mm.logger.Debug().
			Str("oid", oid).
			Msg("OID not found in gosmi, trying fallback")
	}
	
	mm.stats.failedResolutions++
	
	mm.logger.Info().
		Str("oid", oid).
		Int("loaded_mibs", mm.stats.loadedMIBCount).
		Msg("❌ OID not found in any loaded MIB, returning numeric OID")
	
	// Return numeric OID if resolution fails
	resolved := &ResolvedOID{
		OID:         oid,
		Name:        oid,
		Description: "Unknown OID",
		Source:      "numeric",
	}
	
	// Cache negative results too (with shorter TTL)
	mm.cache.Set(oid, resolved)
	
	return resolved
}

// resolveIndexedOID attempts to resolve indexed OIDs (like table entries)
func (mm *MIBManager) resolveIndexedOID(oid string, mib *MIBInfo) *ResolvedOID {
	// Simple implementation - look for base OID matches
	for baseOID, name := range mib.OIDMappings {
		if len(oid) > len(baseOID) && oid[:len(baseOID)] == baseOID {
			// This is an indexed version of the base OID
			index := oid[len(baseOID):]
			if index[0] == '.' {
				index = index[1:] // Remove leading dot
			}
			
			resolved := &ResolvedOID{
				OID:    oid,
				Name:   name + "." + index,
				Source: "embedded",
			}
			
			if desc, exists := mib.Descriptions[baseOID]; exists {
				resolved.Description = desc + " (index: " + index + ")"
			}
			
			return resolved
		}
	}
	
	return nil
}

// GetStats returns MIB manager statistics
func (mm *MIBManager) GetStats() MIBStats {
	mm.mutex.RLock()
	defer mm.mutex.RUnlock()
	
	hitRate := float64(0)
	total := mm.stats.cacheHitCount + mm.stats.cacheMissCount
	if total > 0 {
		hitRate = float64(mm.stats.cacheHitCount) / float64(total) * 100
	}
	
	return MIBStats{
		LoadedMIBCount:     mm.stats.loadedMIBCount,
		CacheSize:          mm.cache.Size(),
		CacheHitRate:       hitRate,
		LastMIBLoadTime:    mm.stats.lastLoadTime,
		OIDResolutionCount: mm.stats.resolutionCount,
		FailedResolutions:  mm.stats.failedResolutions,
	}
}

// CleanCache performs cache maintenance
func (mm *MIBManager) CleanCache() {
	mm.cache.Clean()
}

// loadEmbeddedMIBs loads MIBs that are embedded in the binary
func (mm *MIBManager) loadEmbeddedMIBs() error {
	mm.logger.Debug().Msg("Loading embedded MIBs")
	
	allMIBNames := mibs.GetAllMIBNames()
	loadedCount := 0
	
	for _, mibName := range allMIBNames {
		content, exists := mibs.GetMIBContent(mibName)
		if !exists {
			continue
		}
		
		// Parse the embedded MIB content and extract OID mappings
		mibInfo := &MIBInfo{
			Name:         mibName,
			FilePath:     fmt.Sprintf("embedded:%s", mibName),
			LoadTime:     time.Now(),
			OIDMappings:  make(map[string]string),
			Descriptions: make(map[string]string),
			Units:        make(map[string]string),
		}
		
		// Extract OID mappings from MIB content
		mm.parseEmbeddedMIBContent(content, mibInfo)
		
		mm.loadedMIBs[mibName] = mibInfo
		loadedCount++
		
		mm.logger.Debug().
			Str("mib_name", mibName).
			Int("oid_count", len(mibInfo.OIDMappings)).
			Msg("Loaded embedded MIB")
	}
	
	mm.logger.Info().
		Int("loaded_count", loadedCount).
		Int("total_available", len(allMIBNames)).
		Msg("Embedded MIBs loaded")
	
	return nil
}

// loadExternalMIBs loads MIBs from external files
func (mm *MIBManager) loadExternalMIBs() error {
	if mm.config.ExternalMIBsPath == "" {
		mm.logger.Debug().Msg("No external MIBs path configured")
		return nil
	}
	
	mm.logger.Debug().
		Str("path", mm.config.ExternalMIBsPath).
		Msg("Loading external MIBs")
	
	if _, err := os.Stat(mm.config.ExternalMIBsPath); os.IsNotExist(err) {
		mm.logger.Error().
			Str("path", mm.config.ExternalMIBsPath).
			Err(err).
			Msg("External MIBs path does not exist")
		return fmt.Errorf("external MIBs path does not exist: %s", mm.config.ExternalMIBsPath)
	}
	
	mm.logger.Debug().
		Str("path", mm.config.ExternalMIBsPath).
		Msg("External MIBs path exists, starting directory walk")
	
	loadedCount := 0
	scannedFiles := 0
	skippedDirs := 0
	skippedFiles := 0
	
	err := filepath.Walk(mm.config.ExternalMIBsPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			mm.logger.Debug().
				Err(err).
				Str("path", path).
				Msg("Error during directory walk")
			return err
		}
		
		// Count directories
		if info.IsDir() {
			skippedDirs++
			if path != mm.config.ExternalMIBsPath { // Don't log the root directory
				mm.logger.Debug().
					Str("dir", path).
					Msg("Scanning directory")
			}
			return nil
		}
		
		scannedFiles++
		
		// Only process .mib, .txt files, or files without extension (common for MIB files)
		fileName := strings.ToLower(info.Name())
		hasValidExtension := strings.HasSuffix(fileName, ".mib") ||
			strings.HasSuffix(fileName, ".txt") ||
			!strings.Contains(fileName, ".") // No extension
		
		if !hasValidExtension {
			skippedFiles++
			mm.logger.Debug().
				Str("file", path).
				Str("filename", info.Name()).
				Msg("Skipping file (not .mib, .txt, or no extension)")
			return nil
		}
		
		mm.logger.Debug().
			Str("file", path).
			Msg("Processing MIB file")
		
		if err := mm.loadExternalMIBFile(path); err != nil {
			mm.logger.Warn().
				Err(err).
				Str("file", path).
				Msg("Failed to load external MIB file")
		} else {
			loadedCount++
			mm.logger.Debug().
				Str("file", path).
				Msg("Successfully loaded MIB file")
		}
		
		return nil
	})
	
	if err != nil {
		mm.logger.Error().
			Err(err).
			Str("path", mm.config.ExternalMIBsPath).
			Msg("Failed to scan external MIBs directory")
		return fmt.Errorf("failed to scan external MIBs directory: %w", err)
	}
	
	mm.logger.Info().
		Int("loaded_count", loadedCount).
		Int("scanned_files", scannedFiles).
		Int("skipped_files", skippedFiles).
		Int("scanned_dirs", skippedDirs).
		Str("path", mm.config.ExternalMIBsPath).
		Msg("External MIBs scan completed")
	
	return nil
}

// loadExternalMIBFile loads a single external MIB file
func (mm *MIBManager) loadExternalMIBFile(filePath string) error {
	mm.logger.Debug().
		Str("file", filePath).
		Msg("Starting to load MIB file")
		
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		mm.logger.Debug().
			Err(err).
			Str("file", filePath).
			Msg("Failed to read MIB file")
		return fmt.Errorf("failed to read MIB file %s: %w", filePath, err)
	}
	
	mm.logger.Debug().
		Str("file", filePath).
		Int("content_size", len(content)).
		Msg("Successfully read MIB file")
	
	// Extract MIB name from file content or filename
	mibName := mm.extractMIBName(string(content), filePath)
	if mibName == "" {
		mm.logger.Debug().
			Str("file", filePath).
			Msg("Could not determine MIB name from file")
		return fmt.Errorf("could not determine MIB name from file %s", filePath)
	}
	
	mm.logger.Debug().
		Str("file", filePath).
		Str("mib_name", mibName).
		Msg("Extracted MIB name")
	
	mibInfo := &MIBInfo{
		Name:         mibName,
		FilePath:     filePath,
		LoadTime:     time.Now(),
		OIDMappings:  make(map[string]string),
		Descriptions: make(map[string]string),
		Units:        make(map[string]string),
	}
	
	// Parse MIB content to extract OID mappings
	mm.parseExternalMIBContent(string(content), mibInfo)
	
	mm.loadedMIBs[mibName] = mibInfo
	
	mm.logger.Debug().
		Str("mib_name", mibName).
		Str("file_path", filePath).
		Int("oid_count", len(mibInfo.OIDMappings)).
		Msg("Loaded external MIB file")
	
	return nil
}

// parseEmbeddedMIBContent parses embedded MIB content and extracts OID mappings
func (mm *MIBManager) parseEmbeddedMIBContent(content string, mibInfo *MIBInfo) {
	// This is a simplified parser for our embedded MIBs
	// It looks for NOTIFICATION-TYPE and OBJECT-TYPE definitions
	
	lines := strings.Split(content, "\n")
	currentOID := ""
	currentName := ""
	currentDesc := ""
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		
		// Look for notification or object definitions
		if strings.Contains(line, "NOTIFICATION-TYPE") || strings.Contains(line, "OBJECT-TYPE") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				currentName = parts[0]
			}
		}
		
		// Look for DESCRIPTION
		if strings.Contains(line, "DESCRIPTION") {
			// Extract description text
			if idx := strings.Index(line, `"`); idx != -1 {
				if endIdx := strings.LastIndex(line, `"`); endIdx > idx {
					currentDesc = line[idx+1 : endIdx]
				}
			}
		}
		
		// Look for OID assignments (::= { ... })
		if strings.Contains(line, "::=") && strings.Contains(line, "{") {
			// Simple OID extraction - this could be made more sophisticated
			if matches := regexp.MustCompile(`\{\s*([^}]+)\s*\}`).FindStringSubmatch(line); len(matches) > 1 {
				oidParts := strings.Fields(matches[1])
				if len(oidParts) >= 2 {
					// This is a simplified approach - real MIB parsing would be more complex
					currentOID = mm.buildOIDFromParts(oidParts)
				}
			}
			
			if currentOID != "" && currentName != "" {
				mibInfo.OIDMappings[currentOID] = currentName
				if currentDesc != "" {
					mibInfo.Descriptions[currentOID] = currentDesc
				}
				
				// Reset for next definition
				currentOID = ""
				currentName = ""
				currentDesc = ""
			}
		}
	}
}

// parseExternalMIBContent parses external MIB file content
func (mm *MIBManager) parseExternalMIBContent(content string, mibInfo *MIBInfo) {
	// This would implement a full MIB parser for external files
	// For now, we'll use the same simplified parser as embedded MIBs
	mm.parseEmbeddedMIBContent(content, mibInfo)
}

// extractMIBName extracts MIB name from content or filename
func (mm *MIBManager) extractMIBName(content, filePath string) string {
	// Try to extract from content first
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "DEFINITIONS ::= BEGIN") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}
	
	// Fall back to filename without extension
	baseName := filepath.Base(filePath)
	ext := filepath.Ext(baseName)
	return strings.TrimSuffix(baseName, ext)
}


// resolveFromMIBs attempts to resolve an OID using loaded MIBs
func (mm *MIBManager) resolveFromMIBs(oid string) string {
	// Try exact match first
	for _, mib := range mm.loadedMIBs {
		if name, exists := mib.OIDMappings[oid]; exists {
			return name
		}
	}
	
	// Try prefix matching for indexed OIDs
	for _, mib := range mm.loadedMIBs {
		for baseOID, name := range mib.OIDMappings {
			if strings.HasPrefix(oid, baseOID+".") {
				index := oid[len(baseOID)+1:]
				return name + "." + index
			}
		}
	}
	
	return ""
}