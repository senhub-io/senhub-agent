# Redfish Tag Enhancement System

## 🎯 Overview

The Redfish probe now includes an intelligent tag enhancement system that automatically improves metric organization and readability. This system addresses the complexity of raw Redfish data by:

- **Adding collection tags** for rapid filtering
- **Simplifying complex identifiers** (sensor names, controllers, drives)
- **Removing redundant information** 
- **Standardizing naming conventions**

## 🏷️ Collection Tag System

Every metric now includes a `collection` tag that categorizes metrics into logical groups:

### Available Collections

| Collection | Description | Example Metrics |
|------------|-------------|-----------------|
| `system` | General system health, power state, hardware info | `hardware.system.health`, `hardware.system.power.state` |
| `thermal` | Temperature sensors, fans, cooling systems | `Temperature sensor_temp_ctrl_A.4`, `Fan Fan 2 Speed` |
| `power` | Power supplies, power consumption, PSU health | `PSU PSU 0, Left Health`, `Power consumption` |
| `processor` | CPU hardware, processor health and summary | `hardware.system.cpu.count`, `hardware.system.cpu.health` |
| `memory` | RAM hardware, memory capacity and health | `hardware.system.memory.size`, `hardware.system.memory.health` |
| `storage` | RAID controllers, storage pools, volumes | `Pool A Used Percent`, `Volume VD1-ME5024 Health` |
| `drives` | Individual drive health, capacity, operations | `Drive 0.11 Health`, `Drive 0.9 Operation Progress` |
| `networkadapter` | Network interface cards and connectivity | Network-related metrics |

## 🔧 Tag Simplification Examples

### Before Enhancement
```json
{
  "channel": "Temperature sensor_temp_ctrl_A.4",
  "tags": {
    "sensor_name": "sensor_temp_ctrl_A.4",
    "controller_id": "controller_a", 
    "controller_name": "controller_a",
    "serial_number": "CN0TYNP0SGW004AS000NA00",
    "endpoint": "https://lb-me5024mgmt1.batistyl.fr"
  }
}
```

### After Enhancement
```json
{
  "channel": "Temperature sensor_temp_ctrl_A.4", 
  "tags": {
    "collection": "thermal",
    "sensor_name": "Controller A Sensor 4",
    "controller": "A",
    "endpoint": "https://lb-me5024mgmt1.batistyl.fr"
  }
}
```

## 🎛️ Filtering Examples

### By Collection (Primary Filter)
```bash
# All thermal metrics (temperatures + fans)
collection=thermal

# All storage metrics (pools + volumes + drives)  
collection=storage

# All power-related metrics
collection=power
```

### Combined Filters
```bash
# All temperatures from Controller A
collection=thermal AND controller=A

# All drive health metrics
collection=drives AND metric_name=*Health*

# All Pool metrics
collection=storage AND pool_name=*Pool*
```

## 📊 Tag Simplification Rules

### Sensor Names
- `sensor_temp_ctrl_A.4` → `Controller A Sensor 4`
- `sensor_temp_iom_0.A.1` → `IOM A Sensor 1` 
- `sensor_temp_psu_0.0.0` → `PSU 0 Sensor 0`

### Controller Names
- `controller_a` → `A`
- `Controller A` → `A`
- `controller_b` → `B`

### Drive Names  
- `0.11` → `Drive 11`
- `0.5` → `Drive 5`

### Pool Names
- `dgA01` → `Pool A`
- `A` → `Pool A`

### Removed Tags
- Very long serial numbers (>20 chars)
- Empty values
- Internal debugging tags (starting with `_`)

## 🚀 Benefits

### For Users
1. **Rapid Filtering**: Use `collection=thermal` to see all temperature/fan metrics
2. **Cleaner Interface**: Simplified names like "Controller A Sensor 4" vs "sensor_temp_ctrl_A.4"
3. **Logical Grouping**: Related metrics grouped together
4. **Reduced Clutter**: Redundant and noisy tags removed

### For Monitoring Tools
1. **PRTG**: Filter by collection for channel organization
2. **Grafana**: Use collection tags for dashboard filtering
3. **Alerts**: Target specific collections (e.g., all thermal alerts)
4. **Dashboards**: Create collection-specific views

## 🛠️ Configuration

The tag enhancement system is **automatically enabled** for all Redfish probes. No configuration required.

### Recommended Probe Configuration
```yaml
# probes.d/10-redfish.yaml — each file under probes.d/ is a YAML array of probes
- name: redfish
  type: redfish
  params:
    endpoint: "https://your-server/redfish/v1/"
    username: "monitoring"
    password: ${secret:redfish.password}   # auto-sealed on install
    collections:  # Optional - specify which collections to monitor
      - system     # General system health
      - thermal    # Temperatures and fans
      - power      # Power supplies
      - storage    # Storage systems
      - drives     # Individual drives
```

## 📈 Integration Examples

### PRTG Channel Organization
```
📂 System Health (collection=system)
   • System Health
   • Power State
   
📂 Thermal Management (collection=thermal)  
   • Controller A Sensor 4 Temperature
   • Controller B Sensor 6 Temperature
   • Fan 2 Speed
   
📂 Storage Performance (collection=storage)
   • Pool A Used Percent
   • Volume VD1-ME5024 Health
```

### Grafana Dashboard Filters
```
Collection: [thermal, power, system]
Controller: [A, B, *]  
Component: [Pool, Drive, Volume]
```

This enhancement system transforms complex Redfish data into an organized, user-friendly monitoring experience while maintaining full compatibility with existing configurations.