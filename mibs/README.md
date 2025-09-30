# SNMP MIBs Collection

This directory contains MIB files sourced from [LibreNMS](https://github.com/librenms/librenms) for use with the SNMP Trap probe.

## Directory Structure

```
mibs/
├── standard/          # Standard RFC and IANA MIBs
├── cisco/            # Cisco Systems MIBs
├── paloaltonetworks/ # Palo Alto Networks MIBs
├── fortinet/         # Fortinet MIBs
├── juniper/          # Juniper Networks MIBs
├── f5/               # F5 Networks MIBs
├── hp/               # HPE (Hewlett Packard Enterprise) MIBs
├── dell/             # Dell MIBs
├── huawei/           # Huawei MIBs
└── arista/           # Arista Networks MIBs
```

## Usage with SNMP Trap Probe

### Configuration

To use these MIBs with the SNMP Trap probe, configure the `external_mibs_path` parameter:

```yaml
- name: snmptrap
  params:
    listen_address: "0.0.0.0:162"
    mib_enrichment:
      enabled: true
      external_mibs_path: "/path/to/senhub-agent/mibs"
      cache_size: 10000
      cache_ttl: "24h"
```

### Vendor-Specific Configuration

For environments with specific vendors, you can point to a vendor-specific directory:

```yaml
# Cisco-only environment
mib_enrichment:
  enabled: true
  external_mibs_path: "/path/to/senhub-agent/mibs/cisco"

# Multi-vendor environment with custom selection
mib_enrichment:
  enabled: true
  external_mibs_path: "/path/to/senhub-agent/mibs"
```

## MIB Statistics

- **Total MIBs**: 1213 files
- **Standard MIBs**: 8 essential RFC MIBs  
- **Vendor MIBs**: 1205 vendor-specific MIBs across 31 vendors
- **Source**: LibreNMS community project

## Standard MIBs Included

| MIB Name | Purpose |
|----------|---------|
| SNMPv2-MIB | Core SNMP v2 definitions |
| SNMPv2-SMI | Structure of Management Information |
| SNMPv2-TC | Textual Conventions |
| IF-MIB | Interface definitions |
| IP-MIB | Internet Protocol definitions |
| HOST-RESOURCES-MIB | Host resources monitoring |
| ENTITY-MIB | Physical entity definitions |
| BRIDGE-MIB | Bridge/switch definitions |

## Vendor Coverage (31 Vendors, 1213 MIBs)

### 🏢 Enterprise Network (Big 5)
- **Cisco** (277 MIBs) - Environmental monitoring, HSRP, port security
- **Huawei** (240 MIBs) - Comprehensive enterprise networking
- **HPE** (98 MIBs) - ProCurve, Aruba integration, data center
- **Dell** (61 MIBs) - Force10, PowerConnect, enterprise switches
- **Fortinet** (39 MIBs) - FortiGate, FortiManager, FortiAnalyzer

### 📡 Wireless & Campus (Aruba Leadership)
- **Aruba CX** (35 MIBs) - Next-gen campus switching
- **ArubaOS** (25 MIBs) - Wireless controllers and APs
- **Juniper** (13 MIBs) - High-end routing and switching
- **Arista** (8 MIBs) - Data center and cloud networking

### 🏠 SMB & SOHO (Complete Coverage)
- **D-Link** (82 MIBs) - Extensive SMB switch coverage
- **TP-Link** (20 MIBs) - Consumer and business equipment
- **Netgear** (4 MIBs) - SOHO and SMB switches
- **MikroTik** (1 MIB) - RouterOS comprehensive management

### 🔒 Security & Firewalls
- **WatchGuard** (13 MIBs) - Firewall and security appliances
- **Palo Alto Networks** (7 MIBs) - Next-gen firewalls, threat intelligence
- **SonicWall** (4 MIBs) - SMB and enterprise firewalls
- **Barracuda** (3 MIBs) - Security and application delivery
- **Check Point** (1 MIB) - Enterprise security gateways

### ⚡ Load Balancers & ADC  
- **F5 Networks** (9 MIBs) - BIG-IP system, global traffic management
- **A10** (included) - Thunder ADC series
- **Kemp** (included) - LoadMaster appliances
- **Radware** (included) - Application delivery controllers

### 🌐 Legacy & Specialized
- **Brocade** (included) - Data center switching (pre-Broadcom)
- **Extreme Networks** (included) - Campus and data center
- **Alcatel-Lucent/Nokia** (included) - Service provider equipment
- **Nortel** (included) - Legacy enterprise networks
- **Gigamon** (included) - Network visibility and monitoring
- **Infoblox** (included) - DNS, DHCP, IP address management

## MIB Loading Process

1. **Startup**: The SNMP Trap probe loads embedded MIBs first
2. **External Scan**: Scans the configured `external_mibs_path` directory
3. **Recursive Loading**: Loads all `.mib` files found in subdirectories
4. **Parsing**: Extracts OID mappings and descriptions
5. **Caching**: Stores translations in LRU cache for performance

## Performance Considerations

### Memory Usage
- Each MIB file typically uses 10-100KB of memory
- Total collection: ~80MB loaded memory footprint
- LRU cache: Additional 1-15MB depending on cache_size

### Loading Time
- Standard MIBs: ~100ms
- Full collection (1213 MIBs): ~8-15 seconds
- Subsequent startups use cached mappings

### Recommendations by Environment Size

#### Small Environment (<50 devices)
```yaml
external_mibs_path: "/path/to/mibs/standard"
cache_size: 1000
```

#### Medium Environment (50-500 devices)
```yaml
external_mibs_path: "/path/to/mibs"
cache_size: 5000
```

#### Large Environment (>500 devices)
```yaml
external_mibs_path: "/path/to/mibs"
cache_size: 30000
cache_ttl: "1h"
```

## Troubleshooting

### MIB Loading Issues

#### Check MIB Syntax
```bash
# Validate MIB syntax (if smilint is available)
smilint /path/to/mib/file.mib
```

#### Verify File Permissions
```bash
# Ensure files are readable
find /path/to/mibs -name "*.mib" ! -readable
```

#### Enable Debug Logging
```bash
./agent run --verbose --debug-modules probe.snmptrap
```

### Common Issues

1. **MIB Dependencies**: Some MIBs require imports from other MIBs
2. **File Extensions**: Ensure files have proper extensions (.mib)
3. **Directory Structure**: Nested directories are supported
4. **Parse Errors**: Some vendor MIBs may have syntax issues

## Updating MIBs

### From LibreNMS
```bash
# Update from LibreNMS repository
git clone https://github.com/librenms/librenms.git /tmp/librenms-update
cp -r /tmp/librenms-update/mibs/* /path/to/senhub-agent/mibs/
```

### From Vendor Sites
- Download latest MIBs from vendor support sites
- Place in appropriate vendor subdirectory
- Restart SNMP Trap probe to reload MIBs

## License

These MIBs are sourced from the LibreNMS project and various vendors.
- Standard RFCs: Public domain
- Vendor MIBs: Subject to respective vendor licensing terms
- LibreNMS collection: GNU General Public License

## Contributing

To add new vendor MIBs:

1. Create vendor subdirectory: `mkdir mibs/newvendor`
2. Add MIB files with proper extensions
3. Update enterprise mappings in probe configuration
4. Test with actual traps from vendor equipment

## Support

For issues with specific MIBs:
1. Check LibreNMS documentation: https://docs.librenms.org/
2. Verify MIB syntax and dependencies
3. Enable probe debug logging for detailed parsing information