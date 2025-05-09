# Redfish Probe

The Redfish probe allows the SenHub agent to monitor hardware systems that implement the [Redfish API](https://www.dmtf.org/standards/redfish), a standardized RESTful interface for managing server hardware. This probe can collect various metrics from supported hardware systems, including temperature, power consumption, fan speeds, component health, and more.

## Features

- Vendor detection with specialized collectors for different hardware manufacturers
- Support for Dell, HPE, Lenovo, and Cisco servers
- Fallback to generic collector for other Redfish-compatible systems
- Collection of various metric types (system, thermal, power, processor, memory, storage, network)
- Proper authentication and session management
- Support for SSL verification control

## Configuration

The Redfish probe requires the following configuration parameters:

```yaml
probes:
  redfish:
    enabled: true
    interval: 300                # Collection interval in seconds (default: 300)
    endpoint: "https://server-bmc.example.com"  # Required: Redfish API endpoint
    username: "admin"            # Required: Username for authentication
    password: "password123"      # Required: Password for authentication
    verify_ssl: true             # Optional: Whether to verify SSL certificates (default: true)
    collections:                 # Optional: Array of collection types to gather
      - system
      - thermal
      - power
      - processor
      - memory
      - storage
      - drives
      - networkadapter
```

### Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `enabled` | boolean | Yes | - | Whether the probe is enabled |
| `interval` | integer | No | 300 | Collection interval in seconds |
| `endpoint` | string | Yes | - | URL of the Redfish API endpoint |
| `username` | string | Yes | - | Username for authentication |
| `password` | string | Yes | - | Password for authentication |
| `verify_ssl` | boolean | No | true | Whether to verify SSL certificates |
| `collections` | array | No | [system, thermal, power, processor, memory] | Collection types to gather |

### Supported Collection Types

| Collection Type | Description | Data Collected |
|-----------------|-------------|----------------|
| `system` | System information | Hardware model, serial number, manufacturer, health status |
| `thermal` | Thermal metrics | Temperature readings, fan speeds, cooling status |
| `power` | Power metrics | Power consumption, power supply status |
| `processor` | CPU information | Processor model, speed, cores, utilization |
| `memory` | Memory information | Memory module details, capacity, status |
| `storage` | Storage controller information | Storage controller status, capacity |
| `drives` | Physical disk information | Disk details, status, capacity |
| `networkadapter` | Network adapter information | Network adapter model, status, ports |

## Example Configurations

### Minimal Configuration

```yaml
probes:
  redfish:
    enabled: true
    endpoint: "https://server-bmc.example.com"
    username: "admin"
    password: "password123"
```

### HPE iLO Configuration

```yaml
probes:
  redfish:
    enabled: true
    interval: 600
    endpoint: "https://ilo-server.example.com"
    username: "admin"
    password: "password123"
    verify_ssl: true
    collections:
      - system
      - thermal
      - power
      - processor
      - memory
      - storage
```

### Dell iDRAC Configuration

```yaml
probes:
  redfish:
    enabled: true
    interval: 300
    endpoint: "https://idrac-server.example.com/redfish"
    username: "root"
    password: "calvin"
    verify_ssl: false  # Self-signed certificates are common in iDRAC
    collections:
      - system
      - thermal
      - power
      - processor
      - memory
```

### Cisco UCS Configuration

```yaml
probes:
  redfish:
    enabled: true
    interval: 300
    endpoint: "https://ucs-server.example.com"
    username: "admin"
    password: "password"
    collections:
      - system
      - thermal
      - power
```

## Vendor Support

The probe automatically detects the server vendor and uses the appropriate collector implementation:

- **Dell** - Optimized for iDRAC Redfish API
- **HPE** - Optimized for iLO Redfish API
- **Lenovo** - Optimized for XClarity Redfish API
- **Cisco** - Optimized for UCS Redfish API
- **Generic** - Fallback for any Redfish-compliant server

## Troubleshooting

Common issues:

1. **Connection failures**
   - Verify the endpoint URL is correct and includes the proper protocol (https://)
   - Check that the BMC/server management interface is reachable from the agent
   - Verify network connectivity and firewall rules

2. **Authentication issues**
   - Verify username and password are correct
   - Check if the user account has sufficient privileges
   - Some BMCs may have account lockout policies after failed attempts

3. **SSL certificate issues**
   - Many BMCs use self-signed certificates
   - Set `verify_ssl: false` if you're using self-signed certificates
   - For production, consider using proper certificates

4. **Missing data**
   - Some vendors may not implement all Redfish collections
   - Check server documentation for supported Redfish features
   - Try different collection types to see what's available

## Limitations

- Not all hardware vendors implement the Redfish standard identically
- Some metrics may be unavailable on certain hardware platforms
- Performance data may be limited compared to vendor-specific monitoring tools
- High polling frequency may impact BMC performance

## Vendor-Specific Notes

### Dell iDRAC

- Dell iDRAC typically provides the most complete Redfish implementation
- Best results with iDRAC 8 or newer
- The API endpoint is usually `https://<idrac-ip>/redfish/v1`

### HPE iLO

- Works best with iLO 4 and newer
- The API endpoint is usually `https://<ilo-ip>/redfish/v1`
- Some advanced metrics may require additional licensing

### Lenovo XClarity

- Supports XClarity controller implementations
- The API endpoint is usually `https://<xcc-ip>/redfish/v1`

### Cisco UCS

- Limited support for some metric types
- The API endpoint format may vary based on UCS version