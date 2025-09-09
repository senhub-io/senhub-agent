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

## Design Patterns & Best Practices

### 🏗️ **Modular Architecture Pattern**
The HTTP strategy follows a modular architecture with specialized managers:

```go
type HTTPSyncStrategy struct {
    // Core modules
    authManager      *AuthenticationManager  // Authentication & security
    healthManager    *HealthManager          // Health checks & monitoring
    apiManager       *APIManager             // API endpoints (PRTG, SenHub, Info)
    webInterface     *WebInterface           // Web UI handlers
    debugManager     *DebugManager           // Debug & admin utilities
    configManager    *ConfigurationManager   // Configuration management
    serverManager    *ServerManager          // HTTP server lifecycle
    utilsManager     *UtilsManager           // Utility functions
    // ... other managers
}
```

**Benefits:**
- Single Responsibility Principle: Each manager handles one concern
- Easier testing and maintenance
- Clear separation of concerns
- Modular development

### 🔄 **Delegation Pattern**
HTTPSyncStrategy delegates to specialized managers instead of handling everything directly:

```go
// ❌ Bad: Handling directly in main strategy
func (h *HTTPSyncStrategy) handlePRTGMetrics(w http.ResponseWriter, r *http.Request) {
    // 100+ lines of PRTG logic here...
}

// ✅ Good: Delegating to specialized manager
func (h *HTTPSyncStrategy) handlePRTGMetrics(w http.ResponseWriter, r *http.Request) {
    h.apiManager.HandlePRTGMetrics(w, r)
}
```

### 🔒 **Encapsulation with Controlled Access**
Provide read-only access to internal modules through getters:

```go
// Module Access Getters (Encapsulation)
// These methods provide controlled access to internal modules

// GetAuthManager returns the authentication manager (read-only access)
func (h *HTTPSyncStrategy) GetAuthManager() *AuthenticationManager {
    return h.authManager
}
```

**Pattern Rules:**
- All getters return pointers for performance (no copying)
- Comment each getter as "(read-only access)"
- Group getters in dedicated section
- Use consistent naming: `Get[ModuleName]Manager()`

### 🏷️ **Module-Specific Logging**
Each module uses its own logger for targeted debugging:

```go
// ✅ Create module-specific logger
moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.http")

// ✅ Pass to managers for consistent logging
authManager := NewAuthenticationManager(agentKey, agentConfig, moduleLogger)
```

**Benefits:**
- Granular log control per module
- Easier debugging with `--debug-modules strategy.http,cache`
- Consistent log format across modules

### 🛠️ **Helper Function Pattern**
Create reusable helper functions for common operations:

```go
// ✅ HTTP Headers Helper
func (w *WebInterface) setNoCacheHeaders(writer http.ResponseWriter) {
    writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
    writer.Header().Set("Pragma", "no-cache")
    writer.Header().Set("Expires", "0")
}

// ✅ Version Parsing Helper
func formatCommitHash(commit string) string {
    // Complex parsing logic centralized here
}
```

**Usage Rules:**
- Helper functions should be pure (no side effects when possible)
- Group related helpers in same file
- Use descriptive names that explain the action
- Document complex helpers with examples

### 🔗 **Configuration Provider Pattern**
Support multiple configuration sources through common interface:

```go
type ConfigurationProvider interface {
    GetName() string
    GetConfiguration() RemoteConfigurationData
    OnConfigChanged(callback func(string))
    Start(chan struct{}) error
    Shutdown(context.Context) error
}

// Implementations:
// - LocalConfiguration (offline mode)
// - RemoteConfiguration (online mode)
```

### 🏷️ **Interface-Based Design**
Define clear interfaces for extensibility:

```go
type AgentConfiguration interface {
    GetAuthenticationKey() string
    GetServerUrl() string
}

// Can be extended with cache config access
type AgentConfigurationWithCache interface {
    AgentConfiguration
    GetCacheConfig() *CacheConfig
}
```

### 📝 **Error Handling Pattern**
Consistent error handling with context:

```go
// ✅ Add context to errors
if err := service.Start(); err != nil {
    return fmt.Errorf("failed to start HTTP server: %w", err)
}

// ✅ Log errors with structured fields
logger.Error().
    Err(err).
    Str("service", serviceName).
    Msg("Failed to start service")
```

### 🧪 **Testing Patterns**
- **Table-driven tests** for multiple scenarios
- **Mock interfaces** for external dependencies
- **Integration tests** for HTTP endpoints
- **Benchmark tests** for performance validation

### 📋 **Code Organization Rules**
1. **File naming**: Use descriptive names (`http_web.go`, `http_api.go`)
2. **Function ordering**: Public functions first, then private helpers
3. **Import grouping**: Standard library, third-party, internal packages
4. **Comment structure**: Package comments, then function documentation
5. **Manager initialization**: Create all managers in constructor, initialize in order of dependencies

### ✅ **Pattern Compliance Checklist**
Before committing new code, verify compliance with our patterns:

#### **Modular Architecture**
- [ ] New functionality added to appropriate manager (not HTTPSyncStrategy directly)
- [ ] Manager follows single responsibility principle
- [ ] Manager initialized in NewHTTPSyncStrategy constructor
- [ ] Manager has proper encapsulation getter: `GetXxxManager()`

#### **Delegation Pattern**
- [ ] HTTPSyncStrategy handlers delegate to managers: `h.apiManager.HandleXxx(w, r)`
- [ ] No business logic in main strategy handlers (only delegation)
- [ ] Comments indicate delegation: `// (delegated to XxxManager)`

#### **Helper Functions**
- [ ] Common operations extracted to helper functions
- [ ] Helper functions are pure (no side effects when possible)
- [ ] Helper functions have descriptive names
- [ ] Complex helpers documented with examples

#### **Logging**
- [ ] Module-specific logger used: `logger.NewModuleLogger(baseLogger, "module.name")`
- [ ] Structured logging with relevant fields
- [ ] Error logging includes context
- [ ] Debug/Info messages provide meaningful information

#### **HTTP Headers**
- [ ] Dynamic HTML pages use `setNoCacheHeaders()`
- [ ] Static assets use appropriate cache headers
- [ ] JSON APIs have consistent headers

#### **Error Handling**
- [ ] Errors wrapped with context: `fmt.Errorf("operation failed: %w", err)`
- [ ] Errors logged with structured fields
- [ ] HTTP errors use appropriate status codes
- [ ] Resource cleanup in error paths

#### **Comments & Documentation**
- [ ] Public functions have descriptive comments
- [ ] Getters commented as "(read-only access)"
- [ ] Complex logic documented with inline comments
- [ ] Interface implementations documented

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
  - Added configurable bind address support for interface selection (loopback, specific IPs)
  - Fixed unit resolution for all metrics by integrating transformer system into cache storage
  - Fixed probe naming consistency issues for proper metrics exposure
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
  - Automatic format selection per endpoint (PRTG=friendly, SenHub=friendly+technical)
  - Health check endpoint at `/health`
  - Graceful shutdown with proper resource cleanup
- CONFIGURATION EXAMPLE:
  ```json
  {
    "storage_config": [{
      "name": "http",
      "params": {
        "port": 8080,
        "endpoints": ["prtg", "senhub"]
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
  - `/internal/agent/services/data_store/transformers/definitions/network.yaml` - Network metrics transformations
  - `/internal/agent/services/data_store/transformers/definitions/cpu.yaml` - CPU metrics transformations
  - `/internal/agent/services/data_store/transformers/definitions/memory.yaml` - Memory metrics transformations
  - `/internal/agent/services/data_store/transformers/definitions/logicaldisk.yaml` - Logical disk metrics transformations
  - `/internal/agent/services/data_store/transformers/definitions/ping_gateway.yaml` - Gateway ping metrics transformations
  - `/internal/agent/services/data_store/transformers/definitions/ping_webapp.yaml` - WebApp ping metrics transformations
  - `/internal/agent/services/data_store/transformers/definitions/load_webapp.yaml` - WebApp load metrics transformations
- REGISTRY UPDATED: 
  - Added "redfish" to probe registry in `/internal/agent/probes/registry.go`
  - Added "winevents" to probe registry in `/internal/agent/probes/registry.go`
  - Added "http" to strategy registry in `/internal/agent/services/data_store/data_store.go`
- DEPENDENCIES ADDED:
  - `github.com/gorilla/mux` for HTTP routing
  - `gopkg.in/yaml.v2` for transformer configuration parsing

## Modular Logging System
- IMPLEMENTED: Full modular logging system based on zerolog with per-component control
- FEATURES:
  - CLI arguments: `--verbose` (all modules) or `--debug-modules "module1,module2"` (selective)
  - Runtime HTTP API: GET/POST `/api/{agentkey}/debug/logs` for viewing/setting log levels
  - 16 predefined modules: agent.*, probe.*, strategy.*
  - Global vs per-module level control (selective mode uses ERROR global + DEBUG for specified modules)
  - All probes migrated to use ModuleLogger for targeted debugging
- DOCUMENTATION: See LOGGING.md for complete usage guide
- BENEFITS: Targeted debugging, reduced log noise, runtime configuration without restart

### Universal Configuration API (COMPLETED)
- OBJECTIVE: Provide universal configuration validation for all probe types and monitoring systems
- PROGRESS: 
  - Implemented Universal Configuration API with three validation levels (schema, connectivity, full)
  - Extended ConfigurationManager following existing patterns and module-based logging
  - Added three new endpoints: `/config/validate`, `/config/preview`, `/config/test` 
  - Implemented probe-specific schema validation for all supported probe types
  - Added network connectivity testing for remote probes (Redfish, WebApp, Syslog)
  - Created mock metrics preview system for full validation mode
  - Added comprehensive HTTP handlers with proper error handling and structured logging
  - Fixed GET/POST method support for Nagios endpoints (HTTP 405 → 100% success rate)
  - Added delegation pattern from HTTPSyncStrategy to ConfigurationManager
  - All tests pass including integration tests (PRTG: 100%, Nagios: 100%)
- FEATURES:
  - **Three validation levels**: 
    - `schema` - Fast structure validation (~1-5ms)
    - `connectivity` - Network connectivity testing (~100-2000ms) 
    - `full` - Complete validation with metrics preview (~1-10s)
  - **Universal probe support**: Redfish, WebApp, System probes, Gateway, Syslog
  - **Monitoring system integration**: Works with PRTG, Nagios, Zabbix, and any monitoring tool
  - **Comprehensive error handling**: Detailed validation results with test-by-test feedback
  - **Security validation**: Authentication endpoint testing and credential verification
  - **Preview metrics**: Sample data collection for verification purposes
- CONFIGURATION EXAMPLE:
  ```bash
  # Validate Redfish configuration with connectivity test
  curl -X POST /api/{key}/config/validate \
    -d '{"probe":"redfish","config":{"endpoint":"https://server.com","username":"admin","password":"secret"},"validation":"connectivity"}'
  
  # Test full configuration with metrics preview
  curl -X POST /api/{key}/config/test \
    -d '{"probe":"redfish","config":{...}}'
  ```
- INTEGRATION:
  - Replaces probe-specific configuration validation scattered across monitoring endpoints
  - Provides standardized validation API for all monitoring systems
  - Enables pre-deployment configuration testing and troubleshooting
  - Supports automated configuration validation in CI/CD pipelines
- DOCUMENTATION: Complete API documentation in `docs/admin-guide/UNIVERSAL-CONFIGURATION.md`

## Offline Mode Implementation (COMPLETED)

### Overview
The SenHub Agent now fully supports **offline mode** for zero-configuration deployment without requiring connectivity to the SenHub platform. This enables deployment in air-gapped environments, edge computing, local testing, and standalone monitoring scenarios.

### Key Features Implemented
- **Local Configuration System**: YAML-based configuration with automatic agent key generation
- **HTTPS/TLS Support**: Auto-generated self-signed certificates or custom certificate support
- **Comprehensive CLI**: Complete command-line interface for offline installation and management
- **Web Interface**: Local dashboard with system overview, API explorer, and administration
- **Multiple API Formats**: PRTG, Nagios, SenHub, and Prometheus-compatible endpoints
- **Certificate Management**: Automatic generation, renewal, and secure storage
- **Service Architecture**: Modified initialization to support both online and offline modes

### Installation Examples

#### Basic Offline Installation
```bash
# HTTP mode (localhost only)
./agent install --offline
./agent start
# Access: http://localhost:8080/web/{agentkey}/dashboard
```

#### HTTPS with Auto-Generated Certificates
```bash
# HTTPS mode with self-signed certificates
./agent install --offline --enable-https
./agent start
# Access: https://localhost:8443/web/{agentkey}/dashboard
```

#### Production HTTPS with Custom Certificates
```bash
# HTTPS with provided certificates
./agent install --offline --enable-https \
  --cert-file /path/to/cert.pem \
  --key-file /path/to/key.pem \
  --https-port 443 \
  --min-tls-version 1.3
```

### CLI Options Added
- `--offline`: Enable offline mode with local configuration
- `--config-path PATH`: Specify configuration file location
- `--enable-https`: Enable HTTPS with TLS encryption
- `--https-port PORT`: Custom HTTPS port (default: 8443)
- `--https-hosts HOSTS`: Hostnames for certificate SAN
- `--cert-file PATH`: Custom TLS certificate file
- `--key-file PATH`: Custom TLS private key file
- `--min-tls-version VER`: Minimum TLS version (1.2, 1.3)

### Architecture Changes
- **ConfigurationProvider Interface**: Unified interface for remote and local configuration
- **LocalConfiguration Service**: YAML-based configuration with agent key generation
- **HTTP Strategy Enhanced**: Full TLS support with multiple certificate modes
- **Service Initialization**: Modified to support offline mode (skips auto-updater)
- **Data Store and Sensor**: Updated to work with both configuration providers

### Generated Configuration Structure
```yaml
agent:
  key: "offline-hostname-timestamp-random"
  mode: offline
  generated: true

storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "senhub", "web", "nagios"]
      tls:  # If HTTPS enabled
        enabled: true
        mode: "auto"
        auto_cert:
          organization: "SenHub Agent"
          common_name: "localhost"
          san_hosts: ["localhost", "127.0.0.1"]
          validity_days: 365

probes:
  - name: cpu
    params: {interval: 30}
  - name: memory
    params: {interval: 30}
  - name: network
    params: {interval: 60}
  - name: logicaldisk
    params: {interval: 30}
# Additional probes available as commented examples
```

### Documentation Created
- **OFFLINE-MODE.md**: Comprehensive offline mode documentation
- **HTTPS-CONFIGURATION.md**: Detailed TLS/HTTPS configuration guide
- **QUICK-START-OFFLINE.md**: 5-minute setup guide for users

### Security Features
- **Agent Key Generation**: Machine fingerprint-based unique keys
- **Self-Signed Certificates**: Automatic generation with configurable SAN
- **TLS 1.2/1.3 Support**: Modern encryption with secure cipher suites
- **Certificate Auto-Renewal**: Automatic renewal before expiration
- **Secure File Permissions**: Proper certificate and key file protection

### Integration Support
- **PRTG Network Monitor**: JSON format with channels and limits
- **Nagios/Icinga**: Status codes and performance data
- **Grafana/Prometheus**: Metrics format for visualization
- **Custom Monitoring**: RESTful APIs for any monitoring system

### Files Created/Modified for Offline Mode
- `internal/agent/services/configuration/localConfiguration.go` - Local YAML configuration system
- `internal/agent/cliArgs/cliArgs.go` - Enhanced CLI arguments with offline options
- `internal/agent/agent.go` - Modified service initialization for offline mode
- `internal/agent/services/data_store/strategy_http.go` - Enhanced with TLS support
- `cmd/agent/main.go` - Updated CLI with offline installation workflow
- `OFFLINE-MODE.md` - Complete offline mode documentation
- `HTTPS-CONFIGURATION.md` - TLS configuration guide
- `QUICK-START-OFFLINE.md` - User quick start guide

## Version Tagging
- IMPORTANT: Version tags should NOT include the "v" prefix (use "0.0.82-beta" instead of "v0.0.82-beta")

## Configuration Management - Strategic Implementation Complete

### Contexte actuel
- Déploiement actuel : AgentKey fourni → Configuration via SenHub Platform
- Processus : Créer agent dans SenHub → Configurer connecteur host → Déployer avec AgentKey
- Limitation : Nécessite connectivité SenHub pour fonctionner

### Vision : Agent autonome
**Objectif** : Agent déployable complètement offline avec génération auto d'AgentKey et configuration minimale

### Modes proposés
1. **Mode Online (actuel)** : Agent → SenHub Platform → Config + Updates
2. **Mode Offline** : Agent → AgentKey auto-généré → Config locale → Autonome
3. **Mode Hybride** : Détection connectivité → Basculement auto online/offline

### Avantages Mode Offline
- ✅ Déploiement zéro-config réseau
- ✅ Environments sécurisés sans Internet
- ✅ Edge computing et IoT
- ✅ Tests locaux sans compte SenHub
- ✅ Résilience : fonctionnement continu même si SenHub indisponible

### Inconvénients potentiels
- ❌ Pas de centralisation des configurations
- ❌ Mises à jour manuelles si offline permanent
- ❌ Gestion AgentKey auto-générées (unicité, révocation)

### Implémentation suggérée

#### Génération AgentKey offline
```go
machineID := getMachineFingerprint() // MAC, hostname, etc.
timestamp := time.Now().Unix()
random := generateSecureRandom(8)
agentKey := fmt.Sprintf("%s-%d-%s", machineID, timestamp, random)
```

#### Configuration par défaut
```yaml
agent:
  key: auto-generated-key-here
  mode: offline  # online, offline, hybrid
probes:
  - name: host
    enabled: true
    config:
      metrics: [cpu, memory, network, logicaldisk]
storage:
  - name: local_files
    params:
      path: ./metrics/
      format: json
```

#### Interface web locale
- Configuration probes sans SenHub
- Gestion connecteurs localement
- Import/Export configurations
- URL: http://localhost:8080/web/{agentkey}/config

### Roadmap proposée

#### Phase 1 : Mode Offline de base
1. Génération AgentKey auto
2. Configuration par défaut avec probe host
3. Storage local (JSON/CSV)
4. Interface web configuration

#### Phase 2 : Mode Hybride
1. Détection connectivité SenHub
2. Basculement automatique online/offline
3. Synchronisation configs quand online
4. Cache local des configurations

#### Phase 3 : Fonctionnalités avancées
1. Export/Import configurations
2. Clustering agents offline
3. Proxy mode (agent online pour plusieurs offline)
4. Configuration discovery automatique

### Questions stratégiques à résoudre
- Modèle économique : offline gratuit vs online premium ?
- Support : identification/support agents offline
- Migration : offline → online, import config locale vers SenHub
- Télémétrie anonyme pour statistics usage

### Conclusion
Mode hybride optimal : combine simplicité offline + puissance online
→ SenHub Agent universellement déployable préservant valeur ajoutée plateforme

**Status** : Réflexion notée - À développer demain

## Version Tagging - Updated Guidelines

### Tag Format Consistency (2025-09-09)
- **IMPORTANT**: All version tags must follow the format `X.Y.Z-beta` (WITHOUT the "v" prefix)
- **Fixed**: Purged all problematic `v0.0.x-beta` tags that caused GoReleaser conflicts
- **Fixed**: Removed regression tag `0.0.75-beta` that was causing version conflicts
- **Current format**: `0.1.x-beta` - continue incrementing from `0.1.56-beta`
- **Beta releases**: Automatically generated from dev branch pushes
- **Workflow**: Uses `git describe --tags --abbrev=0` to find latest tag
- **Next version**: 0.1.57-beta created, ready for beta releases