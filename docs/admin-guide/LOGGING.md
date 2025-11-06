# SenHub Agent - Modular Logging System

## Overview

The SenHub Agent uses a modular logging system based on [zerolog](https://github.com/rs/zerolog) that provides granular control over log levels per component. This system allows enabling/disabling debug logs for specific modules without affecting other components.

## Architecture

### Core Components

1. **Logger** (`*zerolog.Logger`) - Base logger (alias of zerolog.Logger)
2. **ModuleLogger** - Wrapper that adds per-module filtering
3. **Global Log Level** - Global level for all standard loggers
4. **Module Log Levels** - Specific levels per module

### Logger Hierarchy

```
┌─────────────────┐
│ zerolog.Logger  │ ← Base logger (global level)
└─────────────────┘
         │
         ▼
┌─────────────────┐
│ ModuleLogger    │ ← Wrapper with per-module filtering
└─────────────────┘
         │
         ▼
┌─────────────────┐
│ zerolog.Event   │ ← Log events (Debug, Info, Warn, Error)
└─────────────────┘
```

## Predefined Modules

The system defines 16 modules for different components:

| Module | Description |
|--------|-------------|
| `agent.core` | Main agent and orchestration |
| `agent.config` | Configuration and parsing |
| `agent.scheduler` | Task scheduler |
| `probe.cpu` | CPU probe |
| `probe.memory` | Memory probe |
| `probe.network` | Network probe |
| `probe.disk` | Logical disk probe |
| `probe.redfish` | Redfish probe |
| `probe.otel` | OpenTelemetry probe |
| `probe.webapp` | Web application probe |
| `probe.gateway` | Gateway/ping probe |
| `probe.wifi` | WiFi signal probe |
| `probe.syslog` | Syslog probe |
| `probe.event` | Event probe (HTTP endpoint) |
| `pdh.windows` | Windows Performance Data Helper (low-level) |
| `strategy.senhub` | SenHub sending strategy |
| `strategy.prtg` | PRTG sending strategy |
| `strategy.http` | HTTP/cache strategy |

## Usage

### CLI Arguments

#### Full verbose mode (backward compatible)
```bash
./senhub-agent --verbose --authentication-key "..."
```
Enables DEBUG level for all modules.

#### Selective debug mode
```bash
./senhub-agent --debug-modules "strategy.http,probe.redfish" --authentication-key "..."
```
Enables DEBUG level only for specified modules.

### Runtime HTTP API

#### View current log levels
```bash
GET /api/{agentkey}/debug/logs
```

#### Modify log levels
```bash
POST /api/{agentkey}/debug/logs
Content-Type: application/json

{
  "modules": [
    {"module": "probe.redfish", "level": "debug"},
    {"module": "strategy.http", "level": "info"}
  ]
}
```

### Supported Log Levels

- `disabled` - No logs
- `trace` - Detailed tracing
- `debug` - Detailed debugging
- `info` - General information
- `warn` - Warnings
- `error` - Errors only
- `fatal` - Fatal errors
- `panic` - Panics

## Code Implementation

### Using ModuleLogger

```go
// In a probe
type myProbe struct {
    moduleLogger *logger.ModuleLogger
}

func NewMyProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
    // Create module-specific logger
    moduleLogger := logger.NewModuleLogger(baseLogger, "probe.myprobe")
    
    return &myProbe{
        moduleLogger: moduleLogger,
    }, nil
}

func (p *myProbe) someMethod() {
    // Normal usage like zerolog
    p.moduleLogger.Debug().Msg("Debug message - filtered by module level")
    p.moduleLogger.Info().Str("key", "value").Msg("Info message")
    p.moduleLogger.Error().Err(err).Msg("Error message")
}
```

### Converting from standard logger

**Before (standard logger):**
```go
type oldProbe struct {
    logger *logger.Logger
}

func (p *oldProbe) method() {
    p.logger.Debug().Msg("This debug will appear in verbose mode")
}
```

**After (ModuleLogger):**
```go
type newProbe struct {
    moduleLogger *logger.ModuleLogger
}

func NewProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
    moduleLogger := logger.NewModuleLogger(baseLogger, "probe.example")
    return &newProbe{moduleLogger: moduleLogger}, nil
}

func (p *newProbe) method() {
    p.moduleLogger.Debug().Msg("This debug only appears if probe.example is enabled")
}
```

## System Behavior

### Output Destinations

The agent automatically detects the execution mode and routes logs accordingly:

- **Interactive mode** (`./agent run`): Logs to console (stderr) AND file/shipper
- **Service mode** (daemon): Logs only to file/shipper (no console)

### Log Level Behavior

**IMPORTANT**: Info/Warn/Error messages are **ALWAYS** output for all modules, regardless of debug settings. Only Debug logs are filtered by module.

### Verbose mode (`--verbose`)
- **Global level**: DEBUG
- **Modules**: All at DEBUG level
- **Info/Warn/Error**: ALL modules (always visible)
- **Debug**: ALL modules (enabled)
- **Result**: All logs from all components are visible

### Selective mode (`--verbose --debug-modules "module1,module2"`)
- **Global level**: INFO (for non-module logs)
- **Specified modules**: DEBUG
- **Non-specified modules**: INFO
- **Info/Warn/Error**: ALL modules (always visible)
- **Debug**: Only specified modules
- **Result**: Info/Warn/Error from all modules + Debug only from specified modules

### Practical Examples

#### Debug only HTTP cache issues
```bash
./senhub-agent --debug-modules "strategy.http" --authentication-key "..."
```

#### Debug Redfish and network probes
```bash
./senhub-agent --debug-modules "probe.redfish,probe.network" --authentication-key "..."
```

#### Debug Windows performance counters (PDH)
```bash
./senhub-agent --debug-modules "pdh.windows" --authentication-key "..."
```

#### Runtime level changes
```bash
# Enable debug for probe.redfish
curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"modules":[{"module":"probe.redfish","level":"debug"}]}'

# Disable all logs from a module
curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"modules":[{"module":"probe.cpu","level":"disabled"}]}'
```

## Technical Details

### Log Filtering

The `ModuleLogger` filters **only Debug logs** based on module configuration. Info/Warn/Error logs are never filtered:

```go
// Debug logs are filtered by module
func (m *ModuleLogger) Debug() *zerolog.Event {
    if m.selectiveMode {
        if _, enabled := m.enabledModules[m.module]; !enabled {
            disabledLogger := m.Logger.Level(zerolog.Disabled)
            return disabledLogger.Debug()  // Suppressed
        }
    }
    if GetModuleLogLevel(m.module) <= zerolog.DebugLevel {
        return m.Logger.Debug()  // Normal log
    }
    disabledLogger := m.Logger.Level(zerolog.Disabled)
    return disabledLogger.Debug()  // Suppressed log
}

// Info/Warn/Error are NEVER filtered
func (m *ModuleLogger) Info() *zerolog.Event {
    return m.Logger.Info()  // Always enabled
}

func (m *ModuleLogger) Warn() *zerolog.Event {
    return m.Logger.Warn()  // Always enabled
}

func (m *ModuleLogger) Error() *zerolog.Event {
    return m.Logger.Error()  // Always enabled
}
```

### Level Management

```go
// Per-module levels stored in thread-safe map
var moduleLogLevels = make(map[string]zerolog.Level)
var moduleLogLevelsMutex sync.RWMutex

// Get module level
func GetModuleLogLevel(module string) zerolog.Level {
    moduleLogLevelsMutex.RLock()
    defer moduleLogLevelsMutex.RUnlock()
    
    if level, exists := moduleLogLevels[module]; exists {
        return level
    }
    return zerolog.GlobalLevel()  // Fallback to global level
}
```

### Zerolog Integration

The system is fully compatible with zerolog API:

- `ModuleLogger.Debug()` returns `*zerolog.Event`
- `ModuleLogger.Logger` is a `*zerolog.Logger`
- Uses `zerolog.SetGlobalLevel()` for global level
- Supports all zerolog formatters (`.Str()`, `.Int()`, `.Err()`, etc.)

## Migration

### Steps to migrate a component to ModuleLogger

1. **Change logger type**:
   ```go
   // Before
   logger *logger.Logger
   // After  
   moduleLogger *logger.ModuleLogger
   ```

2. **Modify constructor**:
   ```go
   func NewComponent(config map[string]interface{}, baseLogger *logger.Logger) {
       moduleLogger := logger.NewModuleLogger(baseLogger, "module.name")
       return &component{moduleLogger: moduleLogger}
   }
   ```

3. **Adapt log calls**:
   ```go
   // Before
   p.logger.Debug().Msg("message")
   // After
   p.moduleLogger.Debug().Msg("message")
   ```

4. **Choose appropriate module name** following convention:
   - `agent.*` for agent components
   - `probe.*` for probes
   - `strategy.*` for data strategies

## Benefits

1. **Targeted debugging**: Focus on specific components
2. **Performance**: Reduces log volume in production
3. **Flexibility**: Runtime configuration without restart
4. **Backward compatibility**: `--verbose` continues to work
5. **Standard API**: Maintains familiar zerolog API
6. **Thread-safe**: Safe concurrent level management

## Limitations

1. **Complexity**: More complex than simple logging
2. **Memory**: Storage of per-module levels
3. **Convention**: Requires following module naming
4. **Migration**: Manual conversion of existing components

## Common Usage Examples

### Scenario 1: Debug specific cache issues
```bash
# Problem: Metrics not appearing in PRTG endpoint
# Solution: Enable cache and HTTP strategy logs

./senhub-agent --debug-modules "strategy.http" --authentication-key "..."

# Or via API:
curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -d '{"modules":[{"module":"strategy.http","level":"debug"}]}'
```

### Scenario 2: Diagnose Redfish probe issues
```bash
# Problem: Redfish probe not collecting metrics
# Solution: Enable only Redfish logs

./senhub-agent --debug-modules "probe.redfish" --authentication-key "..."
```

### Scenario 3: Reduce log noise in production
```bash
# Problem: Too many logs in production
# Solution: Reduce log levels via API

curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -d '{
    "modules": [
      {"module": "probe.cpu", "level": "error"},
      {"module": "probe.memory", "level": "error"},
      {"module": "probe.network", "level": "warn"}
    ]
  }'
```

## Testing and Validation

### Verify the system works
```bash
# 1. Start agent with specific module
./senhub-agent --debug-modules "strategy.http" --authentication-key "test"

# 2. Verify only strategy.http debug logs appear
# 3. Other components should only show errors

# 4. Test API
curl http://localhost:8080/api/test/debug/logs
```

### Module Naming Convention

Modules follow a hierarchical convention:
- **Top-level**: `agent`, `probe`, `strategy`
- **Sub-modules**: `agent.core`, `probe.cpu`, `strategy.http`

This convention enables granular filtering and logical organization of logs.

## Implementation Notes

### Probe Migration Status

The following probes have been migrated to use ModuleLogger:

- ✅ `probe.cpu` - CPU probe
- ✅ `probe.memory` - Memory probe  
- ✅ `probe.network` - Network probe
- ✅ `probe.disk` - Logical disk probe
- ✅ `probe.redfish` - Redfish probe
- ✅ `probe.otel` - OpenTelemetry probe
- ✅ `probe.webapp` - Web application probes (ping, load)
- ✅ `probe.gateway` - Gateway ping probe
- ✅ `probe.wifi` - WiFi signal strength probe
- ✅ `probe.syslog` - Syslog probe
- ✅ `probe.event` - Event probe (HTTP endpoint)
- ✅ `pdh.windows` - Windows Performance Data Helper utilities

### Strategy Migration Status

- ✅ `strategy.http` - HTTP strategy with cache

### Parameter Naming

To avoid conflicts between the `logger` package and `logger` parameter names, constructors use `baseLogger` as the parameter name:

```go
func NewProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
    moduleLogger := logger.NewModuleLogger(baseLogger, "probe.example")
    // ...
}
```

This prevents Go compiler ambiguity between package and variable names.