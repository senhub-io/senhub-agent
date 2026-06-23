# Architecture

This document describes the architectural design of SenHub Agent, including component structure, interfaces, and design philosophy.

## System Overview

SenHub Agent is a cross-platform monitoring agent built in Go that collects metrics and events from various sources and routes them to different storage strategies.

```
┌─────────────────────────────────────────────────────────┐
│                    SenHub Agent                         │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────┐        ┌──────────────┐              │
│  │  Probes     │───────▶│  DataStore   │              │
│  │             │        │              │              │
│  │ - CPU       │        │ Strategies:  │              │
│  │ - Memory    │        │ - HTTP       │              │
│  │ - Redfish   │        │ - PRTG       │              │
│  │ - Citrix    │        │ - SenHub     │              │
│  │ - ...       │        │ - Event      │              │
│  └─────────────┘        └──────────────┘              │
│                                                         │
│  ┌──────────────────────────────────────┐             │
│  │     Configuration Provider           │             │
│  │  - LocalConfiguration (YAML files)   │             │
│  │    agent.yaml + probes.d + .d/        │             │
│  └──────────────────────────────────────┘             │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Probes

Probes collect metrics and events from various sources. All probes implement the `types.Probe` or `types.ProbeWithCallback` interface.

#### Probe Interface
```go
type Probe interface {
    GetName() string
    GetProbeType() string
    SetName(name string)
    SetProbeType(probeType string)
    Collect() error
    Shutdown(context.Context) error
}
```

#### BaseProbe Pattern
All probes embed `BaseProbe` for consistent behavior:

```go
type BaseProbe struct {
    OnDataPoints data_store.AddCallback
    name         string   // Unique probe instance name
    probeType    string   // Technical probe type
}

type cpuProbe struct {
    *types.BaseProbe  // Embedding provides common functionality
    rawConfig map[string]interface{}
    // ... probe-specific fields
}
```

#### Probe Types
- **System Probes**: cpu, memory, network, logicaldisk
- **Infrastructure Probes**: redfish (server hardware monitoring)
- **Application Probes**: citrix (CVAD monitoring)
- **Network Probes**: gateway (ping), webapp (HTTP monitoring)
- **Event Probes**: syslog, winevents

### 2. DataStore

Routes data points from probes to storage strategies. Acts as a dispatcher with callback registration.

```go
type DataStore struct {
    strategies []Strategy
    logger     zerolog.Logger
}

func (ds *DataStore) AddDataPoints(points []data_store.DataPoint) {
    for _, strategy := range ds.strategies {
        strategy.SendDataPoints(points)
    }
}
```

#### Storage Strategies
- **senhub**: Send data to SenHub platform
- **prtg**: Format for PRTG Network Monitor
- **event**: Process event-based data
- **http**: Expose via HTTP REST API

### 3. Configuration Provider

Unified interface for configuration management:

```go
type ConfigurationProvider interface {
    GetName() string
    GetConfiguration() ConfigurationData
    OnConfigChanged(callback func(string))
    Start(chan struct{}) error
    Shutdown(context.Context) error
}
```

#### Implementations
- **LocalConfiguration**: YAML-based local config (agent.yaml + probes.d/ + strategies.d/)

### 4. HTTP Strategy (Modular Architecture)

The HTTP strategy follows a modular architecture with specialized managers:

```go
type HTTPSyncStrategy struct {
    // Core modules
    authManager      *AuthenticationManager  // Authentication & security
    healthManager    *HealthManager          // Health checks & monitoring
    apiManager       *APIManager             // API endpoints
    webInterface     *WebInterface           // Web UI handlers
    debugManager     *DebugManager           // Debug & admin utilities
    configManager    *ConfigurationManager   // Configuration management
    serverManager    *ServerManager          // HTTP server lifecycle
    utilsManager     *UtilsManager           // Utility functions
}
```

Benefits:
- Single Responsibility Principle
- Easier testing and maintenance
- Clear separation of concerns
- Modular development

## Configuration Architecture

### Configuration Format (v2)

Probe configuration uses a two-level identification system:

```yaml
probes:
  - name: Production Citrix      # Display name (free choice)
    type: citrix                 # Probe type (must match registry)
    params:
      base_url: "https://director.example.com"
      interval: 120

  - name: Backup Citrix          # Different display name
    type: citrix                 # Same probe type
    params:
      base_url: "https://director-backup.example.com"
      interval: 120
```

#### name vs type Distinction

| Aspect | name | type |
|--------|------|------|
| **Purpose** | Unique instance identifier | Technical probe class |
| **Scope** | Per probe instance | Shared across probe class |
| **Example** | "Production Citrix", "cpu2" | "citrix", "cpu" |
| **Used by** | Cache keys, metrics tags | Registry, transformers, discriminant tags |
| **Required** | Yes, must be unique | Yes, must exist in registry |

### Automatic Migration

Zero-downtime migration system:
1. Detects old config format (missing `type` field)
2. Creates timestamped backup
3. Adds `type` field (copies from `name`)
4. Saves migrated config
5. Agent continues startup

Implementation:
```go
// In LocalConfiguration.Start()
migrator := NewConfigMigrator(lc.configPath, lc.logger.Logger)
if err := migrator.MigrateIfNeeded(); err != nil {
    lc.logger.Warn().Err(err).Msg("Configuration migration failed")
}
```

## Data Flow

### Metric Collection Flow
```
Probe.Collect()
    ↓
Generate DataPoints
    ↓
Call OnDataPoints callback
    ↓
DataStore.AddDataPoints()
    ↓
Strategy.SendDataPoints()
    ↓
Storage/Export
```

### Configuration Update Flow
```
Local config file changes (fsnotify watcher)
    ↓
LocalConfiguration reloads + atomically swaps the snapshot
    ↓
Registered callbacks fire
    ↓
Agent applies the new configuration
    ↓
Restart probes with new config
```

## Resource Management

### Lifecycle Management
All components implement proper lifecycle management:

```go
type Service interface {
    Start(stopChan chan struct{}) error
    Shutdown(ctx context.Context) error
}
```

### Cleanup Pattern
```go
func (p *probe) Shutdown(ctx context.Context) error {
    // 1. Stop collection loops
    close(p.stopChan)

    // 2. Wait for goroutines with timeout
    select {
    case <-p.doneChan:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Error Handling
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

## Logging Architecture

### Module-Specific Logging
```go
// Create module-specific logger
moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.http")

// Pass to components
authManager := NewAuthenticationManager(agentKey, agentConfig, moduleLogger)
```

### Log Modules
- `strategy.http`, `strategy.prtg`, `strategy.senhub` - Data routing
- `probe.redfish`, `probe.host`, `probe.citrix` - Data collection
- `cache`, `transformer`, `scheduler`, `configuration` - Core system

Benefits:
- Granular log control per module
- Runtime log level changes
- Easier debugging with `--debug-modules`

## Testing Architecture

### Test Organization
```
component.go
component_test.go     # Unit tests
component_unix.go     # Platform-specific
component_unix_test.go
```

### Test Patterns
- **Table-driven tests** for multiple scenarios
- **Mock interfaces** for external dependencies
- **Integration tests** for HTTP endpoints
- **Benchmark tests** for performance validation

See [Build System](./build-system.md#testing) for details.

## Security Considerations

### Authentication
- Agent key-based authentication for all API endpoints
- HTTPS/TLS support with certificate management
- Secure credential storage

### Data Protection
- No sensitive data in logs
- Secure file permissions for certificates and keys
- Input validation for all external data

### Network Security
- Configurable bind address (loopback vs public)
- TLS 1.2/1.3 support
- Certificate validation options

## Performance Considerations

### Caching
- Metric cache with configurable TTL
- Connection pooling for HTTP clients
- Inventory caching for expensive API calls

### Concurrency
- Goroutine-per-probe collection model
- Mutex-protected shared resources
- Context-based cancellation

### Resource Limits
- Configurable collection intervals
- Request timeouts
- Maximum retry attempts

## Extensibility

### Adding New Probes
1. Implement `types.Probe` interface
2. Embed `types.BaseProbe`
3. Register in `probes/registry.go`
4. Add transformer definitions
5. Add tests

### Adding New Strategies
1. Implement `Strategy` interface
2. Register in `data_store/data_store.go`
3. Add configuration schema
4. Add tests

### Adding New Endpoints
1. Create handler in appropriate manager
2. Register route in HTTP strategy
3. Add authentication check
4. Add integration tests

## Next Steps

- Review [Design Patterns](./design-patterns.md) for implementation patterns
- Check [Development Workflow](./development-workflow.md) for process
- See [Current Development](./current-development.md) for active work

---

Last updated: 2025-11-06
