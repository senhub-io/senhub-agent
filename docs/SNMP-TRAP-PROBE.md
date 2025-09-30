# SNMP Trap Probe Documentation

## Overview

The SNMP Trap probe is an event-driven monitoring component that collects SNMP trap notifications from network devices. It provides comprehensive trap processing with MIB-based enrichment, enterprise mapping, and intelligent categorization.

## Features

### Core Capabilities
- **SNMPv1/v2c Support**: Complete trap processing for both SNMP versions
- **Event-Driven Architecture**: Listens continuously on UDP port 162
- **MIB Enrichment**: Translates OIDs to human-readable names using embedded MIBs
- **Enterprise Recognition**: Identifies 60+ network equipment vendors automatically
- **Intelligent Categorization**: Classifies traps by category (security, network, etc.)
- **Advanced Filtering**: Source IP, community string, and enterprise OID filtering
- **Rate Limiting**: Protects against trap flooding with configurable limits
- **Callback Integration**: Sends enriched traps directly to configured strategies

### Vendor Support
The probe includes built-in enterprise mappings for major network vendors:

| Category | Vendors |
|----------|---------|
| **Security** | Palo Alto Networks, Fortinet, Check Point, SonicWall, Barracuda |
| **Network** | Cisco, Juniper, Arista, Extreme Networks, Alcatel-Lucent |
| **Load Balancer** | F5 Networks, Citrix, A10 Networks, Kemp Technologies |
| **Server** | Dell, HPE, IBM, Lenovo, Supermicro |
| **Datacenter** | VMware, Nutanix, NetApp, EMC, Pure Storage |
| **Wireless** | Ubiquiti, Ruckus, Meraki, Aruba |
| **WAN** | Silver Peak, Riverbed, Velocloud, Talari |
| **Monitoring** | PRTG, SolarWinds, Nagios, Zabbix |

## Configuration

### Basic Configuration
```yaml
- name: snmptrap
  params:
    listen_address: "0.0.0.0:162"  # Listen on all interfaces, port 162
    buffer_size: 1000              # Internal circular buffer size
```

### Advanced Configuration
```yaml
- name: snmptrap
  params:
    # Network Configuration
    listen_address: "192.168.1.100:162"  # Specific interface
    buffer_size: 5000                     # Larger buffer for high traffic
    
    # Security Configuration
    communities: ["public", "monitoring", "snmp-ro"]  # Allowed community strings
    
    # MIB Configuration
    mib_enrichment:
      enabled: true                       # Enable MIB-based enrichment
      external_mibs_path: "/etc/mibs"     # Optional: External MIB directory
      cache_size: 10000                   # MIB cache size (OID translations)
      cache_ttl: 3600                     # Cache TTL in seconds
    
    # Filtering Configuration
    filters:
      # Source IP filtering (CIDR notation supported)
      allowed_sources:
        - "192.168.1.0/24"
        - "10.0.0.0/8"
        - "172.16.100.50"
      
      # Block specific sources
      blocked_sources:
        - "192.168.1.99"
        - "10.0.0.100/32"
      
      # Enterprise OID filtering (allow only specific vendors)
      allowed_enterprises:
        - "1.3.6.1.4.1.9"      # Cisco
        - "1.3.6.1.4.1.25461"  # Palo Alto
        - "1.3.6.1.4.1.12356"  # Fortinet
      
      # Rate limiting
      rate_limit:
        max_traps_per_minute: 300         # Global rate limit
        per_source_limit: 50              # Per-source rate limit
        cleanup_interval: 60              # Cleanup interval (seconds)
```

### Minimal Configuration
```yaml
- name: snmptrap
  # Uses defaults: 0.0.0.0:162, buffer_size: 1000, MIB enrichment enabled
```

## Data Output

### DataPoint Structure
Each processed trap generates a `datapoint.DataPoint` with the following structure:

```json
{
  "name": "snmp.trap.received",
  "value": 1.0,
  "timestamp": "2025-01-15T10:30:45Z",
  "tags": [
    {"key": "source_host", "value": "192.168.1.50"},
    {"key": "trap_oid", "value": "1.3.6.1.6.3.1.1.5.3"},
    {"key": "trap_name", "value": "linkDown"},
    {"key": "enterprise", "value": "cisco"},
    {"key": "enterprise_full", "value": "Cisco Systems"},
    {"key": "category", "value": "network"},
    {"key": "severity", "value": "critical"},
    {"key": "event_type", "value": "snmp.trap.received"},
    {"key": "message", "value": "Interface GigabitEthernet0/1 changed state to down"}
  ]
}
```

### Tag Descriptions

| Tag | Description | Example |
|-----|-------------|---------|
| `source_host` | Source IP address of the trap sender | `192.168.1.50` |
| `trap_oid` | SNMP trap OID | `1.3.6.1.6.3.1.1.5.3` |
| `trap_name` | Human-readable trap name (if available) | `linkDown` |
| `enterprise` | Vendor short name | `cisco` |
| `enterprise_full` | Full vendor name | `Cisco Systems` |
| `category` | Equipment category | `network`, `security`, `server` |
| `severity` | Estimated severity level | `critical`, `warning`, `info` |
| `event_type` | Always `snmp.trap.received` | `snmp.trap.received` |
| `message` | Enriched trap message | Interface-specific message |

## MIB Management

### Embedded MIBs
The probe includes embedded standard MIBs:
- **RFC MIBs**: SNMPv2-MIB, IF-MIB, IP-MIB, TCP-MIB, UDP-MIB
- **Standard MIBs**: BRIDGE-MIB, ENTITY-MIB, HOST-RESOURCES-MIB
- **Vendor MIBs**: Selected MIBs for major vendors

### External MIBs
Configure external MIB directory for additional vendor-specific MIBs:
```yaml
mib_enrichment:
  enabled: true
  external_mibs_path: "/usr/share/snmp/mibs"  # System MIB directory
```

Supported MIB formats:
- ASN.1 text files (`.mib`, `.txt`)
- SMI format
- Compiled MIBs

### MIB Cache
- **Default cache size**: 1000 OID translations
- **Cache TTL**: 30 minutes
- **Auto-cleanup**: Removes least recently used entries
- **Statistics**: Available via maintenance loop logging

## Trap Processing Flow

1. **Reception**: UDP listener receives trap on port 162
2. **Validation**: Version, community string, and source IP validation
3. **Parsing**: Extract trap OID, varbinds, and metadata
4. **Rate Limiting**: Apply global and per-source rate limits
5. **Enterprise Recognition**: Identify vendor from enterprise OID
6. **MIB Enrichment**: Translate OIDs to readable names
7. **Categorization**: Classify trap by vendor category
8. **Callback**: Send enriched DataPoint to configured strategies

## Performance and Monitoring

### Statistics
The probe tracks comprehensive statistics:
- **Traps received**: Total trap count
- **Traps processed**: Successfully processed traps
- **Traps dropped**: Dropped due to filtering or rate limiting
- **Traps filtered**: Filtered by source/enterprise rules
- **Rate limited**: Dropped due to rate limiting
- **Buffer utilization**: Current buffer usage
- **MIB cache statistics**: Cache hits, misses, size

### Logging
Enable debug logging for detailed trap processing information:
```bash
./agent run --verbose --debug-modules probe.snmptrap
```

Log levels:
- **INFO**: Startup, statistics, configuration changes
- **DEBUG**: Individual trap processing, filtering decisions
- **WARN**: Rate limiting, MIB loading issues
- **ERROR**: Listener failures, processing errors

### Performance Tuning

#### High-Volume Environments
```yaml
buffer_size: 10000              # Larger buffer
rate_limit:
  max_traps_per_minute: 1000    # Higher rate limit
mib_enrichment:
  cache_size: 50000             # Larger MIB cache
```

#### Low-Resource Environments
```yaml
buffer_size: 500                # Smaller buffer
mib_enrichment:
  enabled: false                # Disable MIB enrichment
  cache_size: 500               # Smaller cache
```

## Integration Examples

### PRTG Integration
Configure HTTP strategy for PRTG consumption:
```yaml
storage_config:
  - name: http
    params:
      port: 8080
      endpoints: ["prtg"]
```

Access trap metrics: `GET http://localhost:8080/api/{agentkey}/prtg/metrics`

### SenHub Platform
Traps are automatically sent to SenHub platform when using default configuration:
```yaml
storage_config:
  - name: event    # Sends to SenHub event pipeline
```

### Syslog Integration
Forward enriched traps to syslog:
```yaml
storage_config:
  - name: syslog
    params:
      server: "syslog.company.com:514"
      facility: "local0"
      format: "json"
```

## Troubleshooting

### Common Issues

#### No Traps Received
1. **Check listening interface**: Ensure `listen_address` is correct
2. **Verify firewall**: UDP port 162 must be open
3. **Test with snmptrap**: Send test trap to verify reception
4. **Check device configuration**: Verify trap destination on network devices

#### Traps Filtered Out
1. **Review source filtering**: Check `allowed_sources`/`blocked_sources`
2. **Verify community strings**: Ensure device community matches `communities` list
3. **Check enterprise filtering**: Review `allowed_enterprises` configuration
4. **Monitor statistics**: Use debug logging to see filtering decisions

#### High Memory Usage
1. **Reduce buffer size**: Lower `buffer_size` parameter
2. **Limit MIB cache**: Reduce `mib_enrichment.cache_size`
3. **Increase rate limiting**: Lower `max_traps_per_minute`
4. **Review trap volume**: Check for trap flooding from devices

#### Missing MIB Translations
1. **Add external MIBs**: Configure `external_mibs_path`
2. **Check MIB format**: Ensure MIBs are in supported format
3. **Review MIB dependencies**: Some MIBs require imports
4. **Enable MIB debugging**: Use `--debug-modules probe.snmptrap`

### Test Commands

#### Send Test Trap (SNMPv2c)
```bash
snmptrap -v2c -c public localhost:162 '' 1.3.6.1.6.3.1.1.5.3 \
  1.3.6.1.2.1.1.3.0 t 123456 \
  1.3.6.1.6.3.1.1.4.1.0 o 1.3.6.1.6.3.1.1.5.3
```

#### Test UDP Connectivity
```bash
nc -u localhost 162 < /dev/null
echo $?  # Should return 0 if port is open
```

#### Verify MIB Loading
```bash
# Check if external MIBs are loaded
ls -la /usr/share/snmp/mibs/
snmptranslate -Td 1.3.6.1.6.3.1.1.5.3
```

## Security Considerations

### Network Security
- **Firewall rules**: Limit UDP 162 access to known trap senders
- **Interface binding**: Bind to specific interface instead of 0.0.0.0
- **VLANs**: Isolate management traffic on dedicated VLAN

### Authentication
- **Community strings**: Use non-default community strings
- **Source IP filtering**: Restrict trap sources to known devices
- **Rate limiting**: Prevent DoS attacks via trap flooding

### Data Privacy
- **Sensitive data**: Be aware that traps may contain sensitive information
- **Log retention**: Configure appropriate log retention policies
- **Access control**: Restrict access to trap data and logs

## API Reference

### Probe Interface Implementation
```go
func (p *SNMPTrapProbe) GetName() string
func (p *SNMPTrapProbe) ShouldStart() bool
func (p *SNMPTrapProbe) GetInterval() time.Duration  // Returns 0 (event-driven)
func (p *SNMPTrapProbe) Collect() ([]datapoint.DataPoint, error)  // Returns empty
func (p *SNMPTrapProbe) SetCallback(callback func([]datapoint.DataPoint) error)
func (p *SNMPTrapProbe) OnStart(quitChannel chan struct{}) error
func (p *SNMPTrapProbe) OnShutdown(ctx context.Context) error
```

### Configuration Validation
All configuration parameters are validated on startup:
- **Network addresses**: Valid IP:port combinations
- **CIDR ranges**: Valid network ranges for filtering
- **Buffer sizes**: Positive integers within reasonable limits
- **Rate limits**: Positive values with sensible maximums

### Error Handling
The probe implements comprehensive error handling:
- **Network errors**: Automatic listener restart on failure
- **Parsing errors**: Individual trap parsing failures don't stop processing
- **MIB errors**: Missing MIBs don't prevent basic trap processing
- **Rate limiting**: Graceful degradation under high load

## Changelog

### Version 1.0.0 (2025-01-15)
- Initial implementation
- SNMPv1/v2c support
- 60+ vendor enterprise mappings
- MIB-based enrichment
- Advanced filtering and rate limiting
- Comprehensive test coverage
- Integration with SenHub agent architecture