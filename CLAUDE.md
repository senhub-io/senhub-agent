# SenHub Agent Development Guidelines

## Build Commands
- Build all binaries: `make build`
- Build for specific OS: `make build-windows`, `make build-linux`, `make build-darwin`
- Run tests: `make test`
- Run single test: `go test -v ./path/to/package -run TestName`
- Development with live reload: `make watch`
- Clean build artifacts: `make clean`

## Code Style Guidelines
- Formatting: Use gofmt (enforced by pre-commit hook)
- Imports: Standard library first, third-party next, internal last with blank lines between groups
- Naming: PascalCase for exported identifiers, camelCase for unexported
- Error handling: Return errors with context using fmt.Errorf, proper logging with zerolog
- Tests: Table-driven tests with clear test cases and meaningful assertions
- Comments: Package-level comments and function documentation following Go standards
- Types: Implement interfaces explicitly, document struct fields
- Cross-platform code: Split platform-specific code using _unix.go and _windows.go files

## Project Architecture
- Probes collect metrics/events, implement types.Probe or types.ProbeWithCallback interface
- DataStore routes data to strategies (senhub, prtg, event)
- Follow resource management best practices (proper cleanup in Shutdown)
- Use agent config from server with proper validation

## Current Development

### Redfish Probe
- OBJECTIVE: Port Python Redfish monitoring plugin to Go probe with vendor-specific collectors
- PROGRESS: 
  - Created core probe structure and generic collector
  - Implemented Redfish API client with session handling
  - Added vendor detection logic
  - Implemented vendor-specific collectors for Dell, HPE, Lenovo, and Cisco
  - Added specialized storage collector for Dell PowerVault ME5024
  - Added probe to registry
  - Implemented collection-specific metrics (system, thermal, power, processor, memory, storage, network)
  - Added comprehensive unit and integration tests
  - Added documentation in REDFISH-METRICS.md
  - Implemented storage metrics for health, capacity, and performance
  - Added disk operation tracking (rebuilds, formatting, etc.)
- TODO: 
  1. Optimize caching system for performance
  2. Add support for additional vendors (SuperMicro, Fujitsu, etc.)
  3. Extend metrics for additional storage operations

### Windows Event Log Probe
- OBJECTIVE: Create a probe to collect Windows Event Log entries
- PROGRESS:
  - Created core probe structure with Windows-specific implementation
  - Implemented event query builder with filters for channels, event IDs, and levels
  - Added probe to registry as "winevents"
  - Implemented basic tests for configuration parsing
- TODO:
  1. Complete Windows API integration with event subscription
  2. Add proper event XML parsing for all fields
  3. Implement efficient event filtering
  4. Add integration tests with Windows event log
  5. Optimize performance for high-volume event logs

## Development Session Information
- WORK DIRECTORY: `/Users/matthieu/Documents/GitHub/senhub-agent/`
- FILES CREATED:
  - `/internal/agent/probes/redfish/collector_interface.go` - Interface for vendor-specific collectors
  - `/internal/agent/probes/redfish/redfish_client.go` - Redfish API client implementation
  - `/internal/agent/probes/redfish/redfishProbe.go` - Main probe implementation 
  - `/internal/agent/probes/redfish/collector_generic.go` - Generic collector for all vendors
  - `/internal/agent/probes/redfish/collector_dell.go` - Dell-specific collector
  - `/internal/agent/probes/redfish/collector_hpe.go` - HPE-specific collector
  - `/internal/agent/probes/redfish/collector_lenovo.go` - Lenovo-specific collector
  - `/internal/agent/probes/redfish/collector_cisco.go` - Cisco-specific collector
  - `/internal/agent/probes/redfish/classification.go` - Classification system for UI grouping
  - `/internal/agent/probes/event/winevents/wineventsProbe.go` - Windows Event Log probe implementation
  - `/internal/agent/probes/event/winevents/wineventsProbe_windows.go` - Windows-specific implementation
  - `/internal/agent/probes/event/winevents/wineventsProbe_test.go` - Tests for Windows Event Log probe
- REGISTRY UPDATED: 
  - Added "redfish" to probe registry in `/internal/agent/probes/registry.go`
  - Added "winevents" to probe registry in `/internal/agent/probes/registry.go`

## Version Tagging
- IMPORTANT: Version tags should NOT include the "v" prefix (use "0.0.82-beta" instead of "v0.0.82-beta")