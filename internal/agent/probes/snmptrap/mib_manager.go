package snmptrap

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
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
	downloader   *MIBDownloader
	
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

// MIBIndex represents the structure of the MIB index JSON
type MIBIndex struct {
	Version   string                 `json:"version"`
	Generated string                 `json:"generated"`
	Vendors   map[string]VendorEntry `json:"vendors"`
}

// VendorEntry represents a vendor in the MIB index
type VendorEntry struct {
	Name     string     `json:"name"`
	BasePath string     `json:"base_path"`
	MIBs     []MIBEntry `json:"mibs"`
}

// MIBEntry represents a MIB file in the index
type MIBEntry struct {
	Name         string   `json:"name"`
	File         string   `json:"file"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// MIBDownloader handles dynamic MIB downloading from central repository
type MIBDownloader struct {
	baseURL     string
	cacheDir    string
	httpClient  *http.Client
	logger      *logger.ModuleLogger
	index       *MIBIndex // Loaded MIB index
	indexLoaded bool
	indexMutex  sync.RWMutex
}

// VendorInfo represents detected vendor information
type VendorInfo struct {
	Name         string
	EnterpriseOID string
	RequiredMIBs []string
	Detected     bool
	LastSeen     time.Time
}

// DownloadRequest represents a MIB download request
type DownloadRequest struct {
	MIBName     string
	VendorName  string
	URL         string
	CacheKey    string
	Priority    int // 0=highest, 1=normal, 2=low
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
	
	// Initialize MIB downloader
	mm.downloader = NewMIBDownloader("https://eu-west-1.intake.senhub.io/mibs/", moduleLogger)
	
	return mm
}

// LoadMIBs loads MIBs from configured paths using gosmi
// ensureStandardMIBs downloads essential standard MIBs if they're missing
func (mm *MIBManager) ensureStandardMIBs() {
	if mm.config.ExternalMIBsPath == "" {
		return
	}

	resolvedPath := mm.resolveExternalMIBsPath()
	if resolvedPath == "" {
		return
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(resolvedPath, 0755); err != nil {
		mm.logger.Warn().Err(err).Msg("Failed to create MIBs directory")
		return
	}

	// List of essential standard MIBs to download
	// MUST match the list in loadStandardMIBs()
	standardMIBs := []string{
		// Base SNMP MIBs
		"SNMPv2-SMI",
		"RFC1155-SMI",

		// Macro definition MIBs (needed by vendor MIBs)
		"RFC-1212",  // OBJECT-TYPE macro for SNMPv1
		"RFC-1215",  // TRAP-TYPE macro for SNMPv1

		// MIBs depending on base MIBs
		"SNMPv2-TC",
		"SNMPv2-MIB",
		"RFC1213-MIB",
		"IF-MIB",
		"HOST-RESOURCES-MIB",

		// Common vendor MIBs
		"CISCO-SMI",
		"CISCO-ENVMON-MIB",
	}

	mm.logger.Info().
		Str("path", resolvedPath).
		Int("mibs_to_check", len(standardMIBs)).
		Msg("📦 Ensuring standard MIBs are available")

	downloaded := 0
	for _, mibName := range standardMIBs {
		mibPath := filepath.Join(resolvedPath, mibName)

		// Check if MIB already exists
		if _, err := os.Stat(mibPath); err == nil {
			mm.logger.Debug().
				Str("mib", mibName).
				Msg("✅ Standard MIB already exists")
			continue
		}

		// Download from remote repository
		url := mm.downloader.baseURL + mibName
		mm.logger.Debug().
			Str("mib", mibName).
			Str("url", url).
			Msg("⬇️  Downloading standard MIB")

		resp, err := mm.downloader.httpClient.Get(url)
		if err != nil {
			mm.logger.Warn().
				Err(err).
				Str("mib", mibName).
				Msg("Failed to download standard MIB")
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			mm.logger.Warn().
				Int("status", resp.StatusCode).
				Str("mib", mibName).
				Msg("Standard MIB not available on server")
			continue
		}

		// Read content
		content, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			mm.logger.Warn().
				Err(err).
				Str("mib", mibName).
				Msg("Failed to read standard MIB content")
			continue
		}

		// Write to file
		if err := ioutil.WriteFile(mibPath, content, 0644); err != nil {
			mm.logger.Warn().
				Err(err).
				Str("mib", mibName).
				Msg("Failed to write standard MIB file")
			continue
		}

		downloaded++
		mm.logger.Info().
			Str("mib", mibName).
			Int("size", len(content)).
			Msg("✅ Downloaded standard MIB")
	}

	if downloaded > 0 {
		mm.logger.Info().
			Int("downloaded", downloaded).
			Int("total", len(standardMIBs)).
			Msg("📥 Standard MIBs download completed")
	}
}

func (mm *MIBManager) LoadMIBs() error {
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
	gosmi.Init()

	// Load external MIBs if path is configured
	// Laisser gosmi scanner et charger ce qu'il veut
	if mm.config.ExternalMIBsPath != "" {
		resolvedPath := mm.resolveExternalMIBsPath()
		if resolvedPath != "" {
			mm.logger.Info().
				Str("path", resolvedPath).
				Msg("Setting MIB search paths for gosmi")

			// Set base MIB path
			gosmi.SetPath(resolvedPath)

			// Add vendor subdirectories to search path
			mm.addVendorPathsToGosmi(resolvedPath)

			// Let gosmi load what it finds
			mm.logger.Info().Msg("MIB paths configured - gosmi will load MIBs on-demand during OID resolution")
		} else {
			mm.logger.Warn().
				Str("path", mm.config.ExternalMIBsPath).
				Msg("Could not resolve external MIBs path")
		}
	}

	mm.stats.lastLoadTime = time.Now()
	mm.logger.Info().Msg("MIB loading completed")

	return nil
}

// addVendorPathsToGosmi adds vendor subdirectories to gosmi search paths
func (mm *MIBManager) addVendorPathsToGosmi(basePath string) {
	entries, err := os.ReadDir(basePath)
	if err != nil {
		mm.logger.Debug().Err(err).Msg("Failed to read MIBs directory")
		return
	}

	vendorCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			vendorPath := filepath.Join(basePath, entry.Name())
			gosmi.AppendPath(vendorPath)
			vendorCount++
			mm.logger.Debug().
				Str("vendor", entry.Name()).
				Str("path", vendorPath).
				Msg("Added vendor path to gosmi")
		}
	}

	mm.logger.Info().
		Int("vendor_paths", vendorCount).
		Msg("Vendor MIB paths added to gosmi search")
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
			Msg("OID not found in gosmi, trying dynamic MIB loading")

		// Try to load vendor MIBs dynamically
		if mm.tryLoadVendorMIBsForOID(oid) {
			// Retry resolution after loading vendor MIBs
			if typeOid, err := types.OidFromString(cleanOID); err == nil {
				if node, err := gosmi.GetNodeByOID(typeOid); err == nil {
					module := node.GetModule()

					mm.logger.Info().
						Str("oid", oid).
						Str("name", node.Name).
						Str("module", module.Name).
						Msg("✅ OID resolved after dynamic MIB loading")

					resolved := &ResolvedOID{
						OID:         oid,
						Name:        node.Name,
						Description: node.Description,
						Source:      "gosmi",
						Module:      module.Name,
					}

					if node.Kind != types.NodeUnknown {
						resolved.Type = node.Kind.String()
					}

					mm.cache.Set(oid, resolved)
					return resolved
				}
			}
		}
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

// tryLoadVendorMIBsForOID attempts to dynamically load vendor MIBs for an OID
func (mm *MIBManager) tryLoadVendorMIBsForOID(oid string) bool {
	// Extract enterprise OID from the OID
	enterpriseOID := extractEnterpriseFromOID(oid)
	if enterpriseOID == "" {
		mm.logger.Debug().
			Str("oid", oid).
			Msg("Not an enterprise OID, cannot load vendor MIBs")
		return false
	}

	// Get vendor directory name from enterprise OID
	vendorDir := mm.getVendorDirFromEnterpriseOID(enterpriseOID)
	if vendorDir == "" {
		mm.logger.Debug().
			Str("enterprise_oid", enterpriseOID).
			Msg("Unknown vendor, cannot determine MIB directory")
		return false
	}

	// Construct vendor MIB path
	if mm.config.ExternalMIBsPath == "" {
		return false
	}

	resolvedBasePath := mm.resolveExternalMIBsPath()
	if resolvedBasePath == "" {
		return false
	}

	vendorPath := filepath.Join(resolvedBasePath, vendorDir)
	if _, err := os.Stat(vendorPath); os.IsNotExist(err) {
		mm.logger.Debug().
			Str("vendor_path", vendorPath).
			Msg("Vendor MIB directory does not exist")
		return false
	}

	mm.logger.Info().
		Str("vendor", vendorDir).
		Str("enterprise_oid", enterpriseOID).
		Str("path", vendorPath).
		Msg("🔄 Loading vendor MIBs dynamically")

	// List all MIB files in vendor directory
	entries, err := os.ReadDir(vendorPath)
	if err != nil {
		mm.logger.Warn().
			Err(err).
			Str("path", vendorPath).
			Msg("Failed to read vendor MIB directory")
		return false
	}

	// Load each MIB file
	loadedCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Skip non-MIB files
		name := entry.Name()
		if strings.HasSuffix(name, ".txt") || strings.HasSuffix(name, ".json") {
			continue
		}

		// Try to load the MIB
		mibName := name
		if strings.HasSuffix(name, ".mib") || strings.HasSuffix(name, ".my") {
			mibName = strings.TrimSuffix(strings.TrimSuffix(name, ".mib"), ".my")
		}

		// Check if already loaded
		if _, err := gosmi.GetModule(mibName); err == nil {
			continue // Already loaded
		}

		// Try to load
		gosmi.AppendPath(vendorPath)
		if _, err := gosmi.LoadModule(mibName); err == nil {
			loadedCount++
			mm.logger.Debug().
				Str("mib", mibName).
				Str("vendor", vendorDir).
				Msg("✅ Loaded vendor MIB")
		} else {
			mm.logger.Debug().
				Err(err).
				Str("mib", mibName).
				Msg("Failed to load vendor MIB")
		}
	}

	if loadedCount > 0 {
		mm.logger.Info().
			Int("loaded", loadedCount).
			Str("vendor", vendorDir).
			Msg("✅ Vendor MIBs loaded dynamically")
		mm.stats.loadedMIBCount += loadedCount
		return true
	}

	return false
}

// getVendorDirFromEnterpriseOID maps an enterprise OID to a vendor directory name
func (mm *MIBManager) getVendorDirFromEnterpriseOID(enterpriseOID string) string {
	// Map enterprise OID to directory name
	vendorDirMap := map[string]string{
		"1.3.6.1.4.1.9":     "cisco",
		"1.3.6.1.4.1.11":    "hp",
		"1.3.6.1.4.1.232":   "hp", // HPE uses same directory
		"1.3.6.1.4.1.674":   "dell",
		"1.3.6.1.4.1.2011":  "huawei",
		"1.3.6.1.4.1.6876":  "vmware",
		"1.3.6.1.4.1.14823": "aruba",
		"1.3.6.1.4.1.47196": "arubaos-cx",
		"1.3.6.1.4.1.25506": "comware", // H3C/Comware
		"1.3.6.1.4.1.12356": "fortinet",
		"1.3.6.1.4.1.25461": "paloalto",
		"1.3.6.1.4.1.1916":  "extreme",
		"1.3.6.1.4.1.1991":  "brocade",
	}

	// Normalize OID (remove leading dot)
	normalizedOID := strings.TrimPrefix(enterpriseOID, ".")

	if dir, found := vendorDirMap[normalizedOID]; found {
		return dir
	}

	return ""
}

// extractEnterpriseFromOID extracts enterprise OID from any OID
// Similar to extractEnterpriseFromTrapOID but works with any OID
func extractEnterpriseFromOID(oid string) string {
	const enterprisePrefix = ".1.3.6.1.4.1."

	// Normalize OID
	if !strings.HasPrefix(oid, ".") {
		oid = "." + oid
	}

	if !strings.HasPrefix(oid, enterprisePrefix) {
		return ""
	}

	rest := strings.TrimPrefix(oid, enterprisePrefix)
	parts := strings.Split(rest, ".")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}

	return enterprisePrefix + parts[0]
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

// loadStandardMIBs loads essential MIBs for basic trap processing
func (mm *MIBManager) loadStandardMIBs() error {
	mm.logger.Info().Msg("🔧 Loading standard MIBs for basic trap processing")
	
	// First, ensure gosmi has the right path
	if mm.config.ExternalMIBsPath != "" {
		resolvedPath := mm.resolveExternalMIBsPath()
		if resolvedPath != "" {
			mm.logger.Debug().
				Str("path", resolvedPath).
				Msg("Setting MIB path for standard MIBs")
			gosmi.AppendPath(resolvedPath)
		}
	}
	
	// Also add the downloader cache directory
	if mm.downloader != nil && mm.downloader.cacheDir != "" {
		mm.logger.Debug().
			Str("path", mm.downloader.cacheDir).
			Msg("Adding download cache to MIB path")
		gosmi.AppendPath(mm.downloader.cacheDir)
	}
	
	// Standard MIBs in dependency order (base MIBs first)
	standardMIBs := []string{
		// Base SNMP MIBs (no dependencies)
		"SNMPv2-SMI",
		"RFC1155-SMI",

		// Macro definition MIBs (needed by vendor MIBs)
		"RFC-1212",  // OBJECT-TYPE macro for SNMPv1
		"RFC-1215",  // TRAP-TYPE macro for SNMPv1

		// MIBs depending on base MIBs
		"SNMPv2-TC",
		"SNMPv2-MIB",
		"RFC1213-MIB",
		"IF-MIB",
		"HOST-RESOURCES-MIB",

		// Vendor MIBs (depend on standard MIBs)
		"CISCO-SMI",
		"CISCO-ENVMON-MIB",
	}

	loadedCount := 0
	for _, mibName := range standardMIBs {
		// Check if already loaded
		if _, err := gosmi.GetModule(mibName); err == nil {
			mm.logger.Debug().
				Str("mib", mibName).
				Msg("MIB already loaded, skipping")
			loadedCount++
			continue
		}

		if _, err := gosmi.LoadModule(mibName); err != nil {
			mm.logger.Debug().
				Err(err).
				Str("mib", mibName).
				Msg("Failed to load standard MIB (may not exist)")
		} else {
			loadedCount++
			mm.logger.Info().
				Str("mib", mibName).
				Msg("✅ Loaded standard MIB")
		}
	}
	
	mm.logger.Info().
		Int("loaded", loadedCount).
		Int("attempted", len(standardMIBs)).
		Msg("🏗️ Standard MIBs loading completed")
	
	mm.stats.loadedMIBCount += loadedCount
	return nil
}

// ProcessTrapForDynamicMIBs analyzes a trap and triggers dynamic MIB loading if needed
func (mm *MIBManager) ProcessTrapForDynamicMIBs(trap *ParsedTrap) {
	if mm.downloader == nil {
		return
	}
	
	// Detect vendor from trap
	vendor := mm.downloader.DetectVendorFromTrap(trap)
	if vendor == nil {
		return
	}
	
	mm.logger.Info().
		Str("vendor", vendor.Name).
		Str("enterprise_oid", vendor.EnterpriseOID).
		Msg("🚀 Triggering dynamic MIB download for detected vendor")
	
	// Download vendor MIBs in background
	go func() {
		if err := mm.downloader.DownloadVendorMIBs(vendor); err != nil {
			mm.logger.Warn().
				Err(err).
				Str("vendor", vendor.Name).
				Msg("Failed to download vendor MIBs")
			return
		}
		
		// Load downloaded MIBs into gosmi
		mm.loadDownloadedMIBs(vendor)
	}()
}

// parseMIBImports extracts IMPORTS from a MIB file
func (mm *MIBManager) parseMIBImports(mibPath string) []string {
	data, err := ioutil.ReadFile(mibPath)
	if err != nil {
		return nil
	}

	content := string(data)
	imports := []string{}

	// Find IMPORTS section (pattern: IMPORTS ... FROM ModuleName)
	// Example: IMPORTS
	//   DisplayString FROM SNMPv2-TC
	//   sysDescr FROM SNMPv2-MIB;

	importPattern := regexp.MustCompile(`(?s)IMPORTS\s+(.*?);`)
	matches := importPattern.FindStringSubmatch(content)
	if len(matches) > 1 {
		importSection := matches[1]

		// Extract module names (everything after FROM)
		fromPattern := regexp.MustCompile(`FROM\s+([A-Za-z0-9\-]+)`)
		fromMatches := fromPattern.FindAllStringSubmatch(importSection, -1)

		for _, match := range fromMatches {
			if len(match) > 1 {
				moduleName := strings.TrimSpace(match[1])
				imports = append(imports, moduleName)
			}
		}
	}

	return imports
}

// loadMIBWithDependencies loads a MIB and resolves its dependencies recursively
func (mm *MIBManager) loadMIBWithDependencies(mibName, mibPath, vendorName string, visited map[string]bool) error {
	// Prevent infinite recursion
	if visited[mibName] {
		return nil
	}
	visited[mibName] = true

	// Check if already loaded in gosmi
	if _, err := gosmi.GetModule(mibName); err == nil {
		return nil
	}

	// Parse imports from MIB file
	imports := mm.parseMIBImports(mibPath)

	mm.logger.Debug().
		Str("mib", mibName).
		Int("imports", len(imports)).
		Msg("Parsing MIB dependencies")

	// Load dependencies first
	for _, importName := range imports {
		// Skip if already loaded
		if _, err := gosmi.GetModule(importName); err == nil {
			continue
		}

		mm.logger.Debug().
			Str("mib", mibName).
			Str("dependency", importName).
			Msg("Resolving dependency")

		// Try to find dependency in cache
		depPath := mm.downloader.GetCachedMIBPath(importName, vendorName)
		if depPath == "" {
			// Try to download dependency
			mm.logger.Debug().
				Str("dependency", importName).
				Str("vendor", vendorName).
				Msg("Downloading missing dependency")

			if downloaded, _ := mm.downloader.downloadMIB(importName, vendorName); downloaded {
				depPath = mm.downloader.GetCachedMIBPath(importName, vendorName)
			}
		}

		// Load dependency recursively
		if depPath != "" {
			if err := mm.loadMIBWithDependencies(importName, depPath, vendorName, visited); err != nil {
				mm.logger.Debug().
					Err(err).
					Str("dependency", importName).
					Msg("Failed to load dependency (non-fatal)")
			}
		}
	}

	// Add MIB directory to gosmi path
	cacheDir := filepath.Dir(mibPath)
	gosmi.AppendPath(cacheDir)

	// Now load the MIB itself
	if _, err := gosmi.LoadModule(mibName); err != nil {
		return fmt.Errorf("failed to load MIB %s: %w", mibName, err)
	}

	mm.logger.Debug().
		Str("mib", mibName).
		Msg("✅ Successfully loaded MIB with dependencies")

	return nil
}

// loadDownloadedMIBs loads newly downloaded MIBs into gosmi
func (mm *MIBManager) loadDownloadedMIBs(vendor *VendorInfo) {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()

	mm.logger.Info().
		Str("vendor", vendor.Name).
		Msg("🔄 Loading downloaded MIBs into gosmi")

	loadedCount := 0
	visited := make(map[string]bool) // Track visited MIBs for recursion

	for _, mibName := range vendor.RequiredMIBs {
		cachedPath := mm.downloader.GetCachedMIBPath(mibName, vendor.Name)
		if cachedPath == "" {
			continue
		}

		// Load MIB with automatic dependency resolution
		if err := mm.loadMIBWithDependencies(mibName, cachedPath, vendor.Name, visited); err != nil {
			mm.logger.Warn().
				Err(err).
				Str("mib", mibName).
				Str("path", cachedPath).
				Msg("Failed to load downloaded MIB")
		} else {
			loadedCount++
			mm.logger.Info().
				Str("mib", mibName).
				Str("vendor", vendor.Name).
				Msg("✅ Successfully loaded downloaded MIB")
		}
	}
	
	mm.stats.loadedMIBCount += loadedCount
	
	mm.logger.Info().
		Str("vendor", vendor.Name).
		Int("loaded", loadedCount).
		Int("total", len(vendor.RequiredMIBs)).
		Msg("🎯 Dynamic MIB loading completed")
	
	// Clear cache to force re-resolution with new MIBs
	if loadedCount > 0 {
		mm.logger.Info().Msg("🔄 Clearing OID cache after loading new MIBs")
		mm.cache.Clean()
		
		// Test resolution of a Cisco OID
		testOID := ".1.3.6.1.4.1.9.9.41.1.2.1.1.2"
		if typeOid, err := types.OidFromString(strings.TrimPrefix(testOID, ".")); err == nil {
			if node, err := gosmi.GetNodeByOID(typeOid); err == nil {
				mm.logger.Info().
					Str("test_oid", testOID).
					Str("resolved_name", node.Name).
					Msg("🧪 Test OID resolution after loading MIBs")
			} else {
				mm.logger.Warn().
					Str("test_oid", testOID).
					Err(err).
					Msg("❌ Failed to resolve test OID")
			}
		}
	}
}

// NewMIBDownloader creates a new MIB downloader
func NewMIBDownloader(baseURL string, logger *logger.ModuleLogger) *MIBDownloader {
	// Use mibs/ directory directly (same as existing MIBs)
	execPath, err := os.Executable()
	if err != nil {
		// Fallback to current working directory
		execPath, _ = os.Getwd()
	} else {
		execPath = filepath.Dir(execPath)
	}
	
	cacheDir := filepath.Join(execPath, "mibs")
	os.MkdirAll(cacheDir, 0755)
	
	downloader := &MIBDownloader{
		baseURL:    baseURL,
		cacheDir:   cacheDir,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}

	// Try to load index (online or offline)
	downloader.loadIndex()

	return downloader
}


// loadIndex attempts to load the MIB index from remote or local sources
func (md *MIBDownloader) loadIndex() {
	md.indexMutex.Lock()
	defer md.indexMutex.Unlock()

	// Try to load from remote URL first
	if md.loadIndexFromURL() {
		md.logger.Info().Msg("✅ MIB index loaded from remote repository")
		return
	}

	// Fallback to local index (offline mode)
	if md.loadIndexFromLocal() {
		md.logger.Info().Msg("✅ MIB index loaded from local cache (offline mode)")
		return
	}

	md.logger.Warn().Msg("⚠️ Could not load MIB index, using legacy vendor mappings")
}

// loadIndexFromURL downloads the index from the remote repository
func (md *MIBDownloader) loadIndexFromURL() bool {
	indexURL := md.baseURL + "index.json"

	md.logger.Info().
		Str("url", indexURL).
		Msg("📡 Attempting to download MIB index from remote")

	resp, err := md.httpClient.Get(indexURL)
	if err != nil {
		md.logger.Warn().
			Err(err).
			Str("url", indexURL).
			Msg("❌ Failed to download MIB index from remote")
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		md.logger.Warn().
			Int("status_code", resp.StatusCode).
			Str("url", indexURL).
			Msg("❌ Remote MIB index not available (HTTP error)")
		return false
	}

	// Read and parse JSON
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		md.logger.Warn().
			Err(err).
			Msg("Failed to read MIB index response")
		return false
	}

	var index MIBIndex
	if err := json.Unmarshal(data, &index); err != nil {
		md.logger.Warn().
			Err(err).
			Msg("Failed to parse MIB index JSON")
		return false
	}

	md.index = &index
	md.indexLoaded = true

	// Cache the index locally for offline use
	localIndexPath := filepath.Join(md.cacheDir, "index.json")
	if err := ioutil.WriteFile(localIndexPath, data, 0644); err != nil {
		md.logger.Warn().
			Err(err).
			Msg("Failed to cache MIB index locally")
	} else {
		md.logger.Debug().
			Str("path", localIndexPath).
			Msg("Cached MIB index locally for offline use")
	}

	md.logger.Info().
		Str("version", index.Version).
		Str("generated", index.Generated).
		Int("vendors", len(index.Vendors)).
		Msg("MIB index loaded successfully from remote")

	return true
}

// loadIndexFromLocal loads the index from local cache
func (md *MIBDownloader) loadIndexFromLocal() bool {
	localIndexPath := filepath.Join(md.cacheDir, "index.json")

	md.logger.Debug().
		Str("path", localIndexPath).
		Msg("Attempting to load MIB index from local cache")

	data, err := ioutil.ReadFile(localIndexPath)
	if err != nil {
		md.logger.Debug().
			Err(err).
			Str("path", localIndexPath).
			Msg("Local MIB index not found")
		return false
	}

	var index MIBIndex
	if err := json.Unmarshal(data, &index); err != nil {
		md.logger.Warn().
			Err(err).
			Msg("Failed to parse local MIB index JSON")
		return false
	}

	md.index = &index
	md.indexLoaded = true

	md.logger.Info().
		Str("version", index.Version).
		Str("generated", index.Generated).
		Int("vendors", len(index.Vendors)).
		Msg("MIB index loaded successfully from local cache")

	return true
}

// DetectVendorFromTrap analyzes a trap to detect the vendor based on enterprise OID
func (md *MIBDownloader) DetectVendorFromTrap(trap *ParsedTrap) *VendorInfo {
	if trap.EnterpriseOID == "" {
		return nil
	}

	md.logger.Info().
		Str("enterprise_oid", trap.EnterpriseOID).
		Msg("🔍 Checking enterprise OID for vendor detection")

	normalizedTrapOID := strings.TrimPrefix(trap.EnterpriseOID, ".")

	// Try to use index first if loaded
	md.indexMutex.RLock()
	if md.indexLoaded && md.index != nil {
		md.indexMutex.RUnlock()

		for baseOID, entry := range md.index.Vendors {
			normalizedBaseOID := strings.TrimPrefix(baseOID, ".")

			if normalizedTrapOID == normalizedBaseOID || strings.HasPrefix(normalizedTrapOID, normalizedBaseOID+".") {
				md.logger.Info().
					Str("enterprise_oid", trap.EnterpriseOID).
					Str("matched_base", baseOID).
					Str("vendor_name", entry.Name).
					Msg("✅ Enterprise OID matched vendor in index")

				// Extract MIB names from index
				mibNames := make([]string, len(entry.MIBs))
				for i, mib := range entry.MIBs {
					mibNames[i] = mib.Name
				}

				vendor := &VendorInfo{
					Name:          entry.Name,
					EnterpriseOID: trap.EnterpriseOID,
					RequiredMIBs:  mibNames,
					Detected:      true,
					LastSeen:      time.Now(),
				}

				md.logger.Info().
					Str("vendor", vendor.Name).
					Str("enterprise_oid", trap.EnterpriseOID).
					Str("base_oid", baseOID).
					Strs("required_mibs", vendor.RequiredMIBs).
					Msg("🏢 Detected vendor from trap using index")

				return vendor
			}
		}
	} else {
		md.indexMutex.RUnlock()
	}

	// No vendor found in index
	md.logger.Warn().
		Str("enterprise_oid", trap.EnterpriseOID).
		Msg("❌ Unknown vendor - not found in MIB index")

	return nil
}

// DownloadVendorMIBs downloads and caches MIBs for a detected vendor
func (md *MIBDownloader) DownloadVendorMIBs(vendor *VendorInfo) error {
	md.logger.Info().
		Str("vendor", vendor.Name).
		Strs("mibs", vendor.RequiredMIBs).
		Msg("📥 Starting MIB download for vendor")
	
	downloadedCount := 0
	cachedCount := 0
	
	for _, mibName := range vendor.RequiredMIBs {
		cached, err := md.downloadMIB(mibName, vendor.Name)
		if err != nil {
			md.logger.Warn().
				Err(err).
				Str("mib", mibName).
				Str("vendor", vendor.Name).
				Msg("Failed to download MIB")
			continue
		}
		
		if cached {
			cachedCount++
		} else {
			downloadedCount++
		}
	}
	
	md.logger.Info().
		Str("vendor", vendor.Name).
		Int("downloaded", downloadedCount).
		Int("cached", cachedCount).
		Int("total", len(vendor.RequiredMIBs)).
		Msg("✅ MIB download completed for vendor")
	
	return nil
}

// downloadMIB downloads a single MIB file with caching
func (md *MIBDownloader) downloadMIB(mibName, vendorName string) (bool, error) {
	// Use original MIB name without extension (same as remote repo)
	cacheFile := filepath.Join(md.cacheDir, mibName)

	// Check if already cached and valid
	if md.isCacheValid(cacheFile) {
		md.logger.Debug().
			Str("mib", mibName).
			Str("cache_file", cacheFile).
			Msg("📁 Using cached MIB")
		return true, nil
	}

	// Build download URL using index if available
	var downloadURL string
	md.indexMutex.RLock()
	if md.indexLoaded && md.index != nil {
		// Find the vendor entry and MIB path from index
		for _, entry := range md.index.Vendors {
			for _, mib := range entry.MIBs {
				if mib.Name == mibName || mib.File == mibName {
					downloadURL = fmt.Sprintf("%s%s/%s", md.baseURL, entry.BasePath, mib.File)
					md.logger.Debug().
						Str("mib", mibName).
						Str("vendor_path", entry.BasePath).
						Str("file", mib.File).
						Msg("📍 Using index to build download URL")
					break
				}
			}
			if downloadURL != "" {
				break
			}
		}
	}
	md.indexMutex.RUnlock()

	// Fallback to legacy URL construction if index not available or MIB not found
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("%s%s/%s", md.baseURL, strings.ToLower(vendorName), mibName)
		md.logger.Debug().
			Str("mib", mibName).
			Str("vendor", vendorName).
			Msg("📍 Using legacy URL construction")
	}
	
	md.logger.Debug().
		Str("mib", mibName).
		Str("url", downloadURL).
		Msg("🌐 Downloading MIB from repository")
	
	// Download the MIB
	resp, err := md.httpClient.Get(downloadURL)
	if err != nil {
		return false, fmt.Errorf("failed to download MIB %s: %w", mibName, err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to download MIB %s: HTTP %d", mibName, resp.StatusCode)
	}
	
	// Read response body
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read MIB content for %s: %w", mibName, err)
	}
	
	// Write to cache
	if err := os.WriteFile(cacheFile, content, 0644); err != nil {
		md.logger.Warn().
			Err(err).
			Str("cache_file", cacheFile).
			Msg("Failed to cache MIB file")
		// Continue without caching
	}
	
	md.logger.Info().
		Str("mib", mibName).
		Str("vendor", vendorName).
		Int("size", len(content)).
		Msg("📥 Successfully downloaded MIB")
	
	return false, nil
}

// generateCacheKey generates a unique cache key for a MIB
func (md *MIBDownloader) generateCacheKey(mibName, vendorName string) string {
	input := fmt.Sprintf("%s-%s", vendorName, mibName)
	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])[:16] // Use first 16 chars of hash
}

// isCacheValid checks if a cached MIB file is still valid
func (md *MIBDownloader) isCacheValid(cacheFile string) bool {
	info, err := os.Stat(cacheFile)
	if err != nil {
		return false
	}
	
	// Cache is valid for 24 hours
	cacheAge := time.Since(info.ModTime())
	maxAge := 24 * time.Hour
	
	valid := cacheAge < maxAge
	
	if !valid {
		md.logger.Debug().
			Str("cache_file", cacheFile).
			Dur("age", cacheAge).
			Dur("max_age", maxAge).
			Msg("Cache expired, will re-download")
	}
	
	return valid
}

// GetCachedMIBPath returns the path to a cached MIB file if it exists
func (md *MIBDownloader) GetCachedMIBPath(mibName, vendorName string) string {
	cacheFile := filepath.Join(md.cacheDir, mibName)
	
	if md.isCacheValid(cacheFile) {
		return cacheFile
	}
	
	return ""
}

// CleanCache removes expired cache files
func (md *MIBDownloader) CleanCache() error {
	md.logger.Debug().
		Str("cache_dir", md.cacheDir).
		Msg("🧹 Starting cache cleanup")
	
	files, err := os.ReadDir(md.cacheDir)
	if err != nil {
		return fmt.Errorf("failed to read cache directory: %w", err)
	}
	
	removedCount := 0
	for _, file := range files {
		if !file.IsDir() {
			filePath := filepath.Join(md.cacheDir, file.Name())
			if !md.isCacheValid(filePath) {
				if err := os.Remove(filePath); err != nil {
					md.logger.Warn().
						Err(err).
						Str("file", filePath).
						Msg("Failed to remove expired cache file")
				} else {
					removedCount++
				}
			}
		}
	}
	
	md.logger.Info().
		Int("removed", removedCount).
		Msg("✅ Cache cleanup completed")
	
	return nil
}