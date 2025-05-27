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
- DataStore routes data to strategies (senhub, prtg, event, http)
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

### HTTP Strategy
- OBJECTIVE: Expose agent metrics via HTTP REST API for external monitoring tools (PRTG, etc.)
- PROGRESS:
  - Implemented HTTP sync strategy with gorilla/mux router
  - Created POST endpoint `/api/{agentkey}/prtg/metrics` for PRTG integration
  - Implemented metric caching system with TTL and automatic cleanup
  - Built modular transformer system for user-friendly metric names
  - Created YAML-based configuration files for metric transformations:
    - `redfish_friendly.yaml` - Redfish metrics with friendly names
    - `host_friendly.yaml` - System metrics transformations  
    - `otel_technical.yaml` - OTEL metrics (technical names)
  - Added authentication via agent key in URL path
  - Implemented PRTG JSON response format with channels, units, and limits
  - Configuration emulation for dynamic probe settings (POST body)
  - Added comprehensive unit tests for strategy and transformers
  - Added strategy to data_store.go registry as "http"
- FEATURES:
  - Cache-based metric storage with 5-minute TTL
  - Template-based name transformations: `thermal.cpu.{index}.temperature` → `"CPU Temperature - Processor {index}"`
  - Configurable naming styles per probe: `{"naming": {"redfish": "friendly", "host": "friendly"}}`
  - Health check endpoint at `/health`
  - Graceful shutdown with proper resource cleanup
- CONFIGURATION EXAMPLE:
  ```json
  {
    "storage_config": [{
      "name": "http",
      "params": {
        "port": 8080,
        "naming": {
          "redfish": "friendly",
          "host": "friendly",
          "otel": "technical"
        }
      }
    }]
  }
  ```
- TODO:
  1. Add support for GET endpoints for other monitoring tools
  2. Implement dynamic configuration updates from POST body
  3. Add support for additional transformer patterns
  4. Add Prometheus format support

### Modular Logging System
- OBJECTIVE: Implement granular log level control per module/component to reduce log noise
- PROGRESS:
  - Created module-based logging system with configurable levels per component
  - Added HTTP endpoints for runtime log level management
  - Implemented logger filtering at module level (strategy.http, probe.redfish, etc.)
  - Added comprehensive test coverage for logging functionality
  - Updated HTTP strategy to use module-specific logger
- CONFIGURATION: Supports 16 predefined modules with individual log levels:
  - `strategy.http`, `strategy.prtg`, `strategy.senhub` - Data routing strategies
  - `probe.redfish`, `probe.host`, `probe.network`, `probe.webapp`, `probe.otel`, `probe.gateway`, `probe.syslog` - Data collection probes
  - `cache`, `transformer`, `scheduler`, `configuration` - Core system components
- API ENDPOINTS:
  - `GET /api/{agentkey}/debug/logs` - View current module log levels
  - `POST /api/{agentkey}/debug/logs` - Set module log levels dynamically
- USAGE EXAMPLES:
  ```bash
  # View current log levels
  curl http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs
  
  # Disable HTTP strategy debug logs
  curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs \
    -H "Content-Type: application/json" \
    -d '{"module_levels": [{"module": "strategy.http", "level": "warn"}]}'
  
  # Enable detailed debugging for Redfish probe only
  curl -X POST http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs \
    -H "Content-Type: application/json" \
    -d '{"module_levels": [{"module": "probe.redfish", "level": "debug"}]}'
  ```
- LOG LEVELS: `debug`, `info`, `warn`, `error`, `fatal`, `panic`, `disabled`
- TODO:
  1. Add configuration file support for default module log levels
  2. Implement log level inheritance for sub-modules

## Debugging Guide

### Enable Debug Logging on Startup

#### Full Debug Mode
Use the `--verbose` flag to enable debug logging for all key modules:
```bash
./agent run --authentication-key YOUR_KEY --verbose
```

This automatically enables debug logging for:
- `strategy.http` - HTTP strategy and cache operations
- `cache` - Cache operations and debugging  
- `probe.redfish` - Redfish probe operations
- `configuration` - Configuration loading and parsing
- `scheduler` - Probe scheduling operations

#### Selective Debug Mode
Use `--verbose` with `--debug-modules` to enable debug logging only for specific modules:
```bash
./agent run --authentication-key YOUR_KEY --verbose --debug-modules strategy.http,cache
./agent run --authentication-key YOUR_KEY --verbose --debug-modules probe.redfish
```

Available modules: `strategy.http`, `strategy.prtg`, `strategy.senhub`, `probe.redfish`, `probe.host`, `probe.network`, `probe.webapp`, `probe.otel`, `probe.gateway`, `probe.syslog`, `cache`, `transformer`, `scheduler`, `configuration`

### Runtime Debug Control
You can also change log levels at runtime via HTTP API:
```bash
# Get current log levels
curl -X GET http://localhost:8080/api/{agentkey}/debug/logs

# Set specific modules to debug
curl -X POST http://localhost:8080/api/{agentkey}/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"strategy.http": "debug", "cache": "debug"}'
```

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
  - `/internal/agent/services/data_store/strategy_http.go` - HTTP strategy implementation
  - `/internal/agent/services/data_store/strategy_http_test.go` - HTTP strategy tests
  - `/internal/agent/services/data_store/transformers/transformer.go` - Metric name transformer system
  - `/internal/agent/services/data_store/transformers/transformer_test.go` - Transformer tests
  - `/internal/agent/services/data_store/transformers/redfish_friendly.yaml` - Redfish metric transformations
  - `/internal/agent/services/data_store/transformers/host_friendly.yaml` - Host metric transformations
  - `/internal/agent/services/data_store/transformers/otel_technical.yaml` - OTEL metric transformations
- REGISTRY UPDATED: 
  - Added "redfish" to probe registry in `/internal/agent/probes/registry.go`
  - Added "winevents" to probe registry in `/internal/agent/probes/registry.go`
  - Added "http" to strategy registry in `/internal/agent/services/data_store/data_store.go`
- DEPENDENCIES ADDED:
  - `github.com/gorilla/mux` for HTTP routing
  - `gopkg.in/yaml.v2` for transformer configuration parsing

## Version Tagging
- IMPORTANT: Version tags should NOT include the "v" prefix (use "0.0.82-beta" instead of "v0.0.82-beta")