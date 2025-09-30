# SNMP Trap MIBs Architecture

## Overview

The SNMP Trap probe includes a sophisticated MIB (Management Information Base) system that provides human-readable translations for SNMP trap OIDs. The system supports both embedded MIBs (compiled into the binary) and external MIB files for maximum flexibility.

## Architecture Components

### 1. Embedded MIBs System

The probe includes embedded MIBs for common network equipment vendors and standard SNMP objects:

```go
// Location: internal/agent/probes/snmptrap/mibs/embedded.go
var EmbeddedMIBs = map[string]string{
    // Standard RFC MIBs
    "SNMPv2-MIB": snmpv2MIB,
    "IF-MIB":     ifMIB,
    "IP-MIB":     ipMIB,
    
    // Vendor-specific MIBs
    "CISCO-SMI":          ciscoSMI,
    "PALOALTO-MIB":       paloaltoMIB,
    "FORTINET-CORE-MIB":  fortinetCoreMIB,
    // ... more vendors
}
```

### 2. MIB Manager

The MIB Manager (`mib_manager.go`) orchestrates MIB loading and OID resolution:

- **Loads embedded MIBs** on startup
- **Loads external MIBs** from configured directory
- **Caches OID translations** for performance
- **Provides OID resolution** for trap enrichment

### 3. MIB Cache

LRU cache system for fast OID lookups:

```go
type MIBCache struct {
    cache    map[string]*CachedOID
    lruList  *list.List
    maxSize  int
    mutex    sync.RWMutex
}
```

## Embedded MIB Content

### Standard MIBs

#### SNMPv2-MIB
Contains standard SNMP traps:
```
coldStart NOTIFICATION-TYPE ::= { snmpTraps 1 }        // 1.3.6.1.6.3.1.1.5.1
warmStart NOTIFICATION-TYPE ::= { snmpTraps 2 }        // 1.3.6.1.6.3.1.1.5.2
linkDown NOTIFICATION-TYPE ::= { snmpTraps 3 }         // 1.3.6.1.6.3.1.1.5.3
linkUp NOTIFICATION-TYPE ::= { snmpTraps 4 }           // 1.3.6.1.6.3.1.1.5.4
authenticationFailure ::= { snmpTraps 5 }              // 1.3.6.1.6.3.1.1.5.5
```

#### IF-MIB
Interface-related objects:
```
ifIndex OBJECT-TYPE ::= { ifEntry 1 }                  // 1.3.6.1.2.1.2.2.1.1
ifDescr OBJECT-TYPE ::= { ifEntry 2 }                  // 1.3.6.1.2.1.2.2.1.2
ifOperStatus OBJECT-TYPE ::= { ifEntry 8 }             // 1.3.6.1.2.1.2.2.1.8
ifAdminStatus OBJECT-TYPE ::= { ifEntry 7 }            // 1.3.6.1.2.1.2.2.1.7
```

### Vendor-Specific MIBs

#### Cisco Systems (Enterprise 9)
```
cisco OBJECT IDENTIFIER ::= { enterprises 9 }
ciscoEnvMonTemperatureNotification ::= { cisco 13 4 1 }
ciscoFanFailureNotif ::= { cisco 13 4 3 }
ciscoPowerSupplyFailureNotif ::= { cisco 13 4 2 }
```

#### Palo Alto Networks (Enterprise 25461)
```
paloalto OBJECT IDENTIFIER ::= { enterprises 25461 }
panSessionUtilization ::= { paloaltoMibs 1 1 }
panThreatDetection ::= { paloaltoMibs 1 3 }
panSystemResourceUtilization ::= { paloaltoMibs 1 4 }
```

#### Fortinet (Enterprise 12356)
```
fortinet OBJECT IDENTIFIER ::= { enterprises 12356 }
fgTrapVpnTunState ::= { fortigate 6 2 1 }
fgTrapHaSwitch ::= { fortigate 6 2 2 }
fgTrapVirusDetected ::= { fortigate 6 2 4 }
```

## External MIB Support

### Configuration

```yaml
mib_enrichment:
  enabled: true
  external_mibs_path: "/etc/snmp/mibs"  # Directory containing MIB files
  cache_size: 10000
  cache_ttl: "24h"
```

### Supported MIB File Formats

1. **ASN.1 Text Files** (`.mib`, `.txt`)
   - Standard MIB definition format
   - Human-readable ASN.1 syntax

2. **SMI Format**
   - Structure of Management Information format
   - Industry standard for SNMP MIBs

### Loading Process

1. **Startup**: Load embedded MIBs first
2. **External Scan**: Scan configured directory for `.mib` and `.txt` files
3. **Parsing**: Extract OID mappings and descriptions
4. **Caching**: Store translations in LRU cache

## OID Resolution Process

### Resolution Hierarchy

1. **Cache Lookup**: Check LRU cache first (fastest)
2. **Exact Match**: Look for exact OID in loaded MIBs
3. **Prefix Match**: Look for parent OID with index
4. **Fallback**: Return original OID if no match

### Example Resolution

```
Input:  "1.3.6.1.6.3.1.1.5.3"
Output: "linkDown"

Input:  "1.3.6.1.2.1.2.2.1.8.1"
Output: "ifOperStatus.1" (interface 1 operational status)
```

## Performance Characteristics

### Cache Statistics

- **Default cache size**: 10,000 entries
- **Cache hit ratio**: Typically >90% after warm-up
- **Average lookup time**: <1ms (cached), <10ms (uncached)

### Memory Usage

- **Embedded MIBs**: ~2MB binary overhead
- **Cache**: ~100KB per 1000 cached entries
- **External MIBs**: Variable based on files loaded

## Adding New Vendor MIBs

### 1. Add to Embedded MIBs

Edit `internal/agent/probes/snmptrap/mibs/embedded.go`:

```go
// Add new vendor MIB definition
const newVendorMIB = `
-- NEW-VENDOR-MIB Core Definitions
newVendor OBJECT IDENTIFIER ::= { enterprises 12345 }

newVendorTrapExample NOTIFICATION-TYPE
    STATUS current
    DESCRIPTION "Example trap from new vendor"
    ::= { newVendor 1 1 }
`

// Add to EmbeddedMIBs map
var EmbeddedMIBs = map[string]string{
    // ... existing MIBs
    "NEW-VENDOR-MIB": newVendorMIB,
}
```

### 2. Update Enterprise Mapping

Edit `internal/agent/probes/snmptrap/enterprise_mappings.go`:

```go
var KnownEnterprises = map[string]EnterpriseInfo{
    // ... existing enterprises
    "12345": {Name: "newvendor", FullName: "New Vendor Inc", Category: "network"},
}
```

### 3. Add Vendor-Specific Mappings

```go
// In buildOIDFromParts function
parentMappings := map[string]string{
    // ... existing mappings
    "NEW-VENDOR-MIB": "1.3.6.1.4.1.12345",
}
```

## External MIB Directory Structure

### Recommended Organization

```
/etc/snmp/mibs/
├── standard/
│   ├── SNMPv2-MIB.txt
│   ├── IF-MIB.txt
│   └── IP-MIB.txt
├── vendors/
│   ├── cisco/
│   │   ├── CISCO-ENVMON-MIB.txt
│   │   └── CISCO-PROCESS-MIB.txt
│   ├── paloalto/
│   │   └── PAN-GLOBAL-MIB.txt
│   └── fortinet/
│       └── FORTINET-FORTIGATE-MIB.txt
```

### File Naming Conventions

- Use vendor prefix: `CISCO-*`, `PAN-*`, `FORTINET-*`
- Include MIB purpose: `*-ENVMON-*`, `*-SYSTEM-*`
- Use `.txt` or `.mib` extension

## Troubleshooting MIB Issues

### Common Issues

#### 1. MIB Not Loading
```
ERROR: Failed to load MIB file /path/to/mib.txt: syntax error
```
**Solution**: Check MIB syntax and dependencies

#### 2. OID Not Resolving
```
DEBUG: OID 1.3.6.1.4.1.9999.1.1 not found in MIBs
```
**Solution**: Add MIB for enterprise 9999 or configure external MIB

#### 3. Missing Dependencies
```
WARN: MIB VENDOR-SPECIFIC-MIB depends on VENDOR-SMI which is not loaded
```
**Solution**: Ensure dependent MIBs are available

### Debug Commands

#### Enable MIB Debug Logging
```bash
./agent run --verbose --debug-modules probe.snmptrap
```

#### Test OID Resolution
```bash
# Check if system can resolve OID
snmptranslate -Td 1.3.6.1.6.3.1.1.5.3
```

#### Verify MIB Syntax
```bash
# Validate MIB file syntax
smilint /path/to/mib.txt
```

## Performance Tuning

### High-Volume Environments

```yaml
mib_enrichment:
  enabled: true
  cache_size: 50000        # Larger cache
  cache_ttl: "1h"          # Shorter TTL for updates
```

### Low-Resource Environments

```yaml
mib_enrichment:
  enabled: false           # Disable MIB enrichment
  cache_size: 1000         # Minimal cache
```

### Network-Specific Tuning

```yaml
# For Cisco-heavy environments
mib_enrichment:
  enabled: true
  auto_load_vendor_mibs: true
  external_mibs_path: "/etc/mibs/cisco"
```

## API Reference

### MIB Manager Methods

```go
func (mm *MIBManager) LoadMIBs() error
func (mm *MIBManager) ResolveOID(oid string) *ResolvedOID
func (mm *MIBManager) GetStats() MIBStats
func (mm *MIBManager) CleanCache()
```

### Embedded MIBs Functions

```go
func GetMIBContent(mibName string) (string, bool)
func GetAllMIBNames() []string
func IsStandardMIB(mibName string) bool
func GetVendorMIBs(vendor string) []string
```

## Future Enhancements

### Planned Features

1. **SNMP MIB Compiler**: Built-in MIB compilation from ASN.1
2. **Dynamic MIB Loading**: Runtime MIB addition without restart
3. **MIB Registry**: Central registry for community-contributed MIBs
4. **Auto-Discovery**: Automatic MIB detection based on received traps

### Integration Possibilities

1. **SNMP Walk Integration**: Auto-discover device MIBs
2. **Vendor APIs**: Download latest MIBs from vendor portals
3. **Community Repository**: Share and download MIBs from community

## Conclusion

The SNMP Trap probe's MIB system provides a powerful foundation for translating raw SNMP OIDs into meaningful information. The combination of embedded standard and vendor MIBs ensures immediate functionality, while external MIB support provides unlimited extensibility for specialized environments.