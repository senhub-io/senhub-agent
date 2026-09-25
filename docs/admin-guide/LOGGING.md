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

## Modules

A module name is a dotted path. The set present in a binary depends on which
probes and outputs that build ships, so the authoritative list is the one the
binary itself prints:

```bash
senhub-agent debug-modules-list
```

The families, and what they cover:

| Prefix | Covers | Examples |
|---|---|---|
| `probe.` | one probe type each | `probe.host`, `probe.logicaldisk`, `probe.postgresql`, `probe.snmp_trap`, `probe.otlp_receiver` |
| `strategy.` | one output each | `strategy.http`, `strategy.otlp`, `strategy.senhub`, `strategy.prtg`, `strategy.event` |
| `configuration.` | loading, watching, migrating, sealing | `configuration.local`, `configuration.agent`, `configuration.migrator` |
| `status.` | the status service behind `agent status` and `/info/*` | `status.service`, `status.helper` |
| `transformer`, `lookups`, `data_store` | metric naming, PRTG lookups, routing | — |
| `sensor`, `server`, `lifecycle` | probe scheduling and the agent's own lifecycle | — |
| `service.auto_update` | update checks | — |
| `pdh.windows` | Windows Performance Data Helper (low-level) | — |

A filter matches a module exactly **or by prefix**, so `probe` selects every
probe module and `probe.postgresql` selects one.

## Usage

### CLI Arguments

These are flags of `run`, the verb the service's `ExecStart` uses. To raise
the level of an already-installed service, either add the flag to `ExecStart`
in a unit drop-in and restart, or use the runtime HTTP API below, which needs
no restart at all.

#### Full verbose mode
```bash
senhub-agent run --verbose
```
Enables DEBUG level for all modules.

#### Selective debug mode
```bash
senhub-agent run --filter "strategy.http,probe.postgresql"
```
Enables DEBUG level only for the matching modules. `--filter` implies
`--verbose`; you do not need both.

`--debug-modules` is the **deprecated** spelling of the same flag, kept so
existing unit drop-ins keep working. `--filter` wins when both are given.

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
  "module_levels": [
    {"module": "probe.postgresql", "level": "debug"},
    {"module": "strategy.http", "level": "info"}
  ]
}
```

The key is `module_levels`. A body using any other key decodes to an empty
list: the agent answers `200 {"status":"success"}` and changes nothing.

The GET returns the same shape (`{"module_levels": [...]}`) and lists only the
modules whose level was **overridden** — a module absent from the answer
follows the global level.

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

The agent detects the execution mode and routes logs accordingly:

- **Interactive** (`senhub-agent run` from a shell): console (stderr) **and** a
  log file **and** the debug shipper when one is configured.
- **Service** (daemon): the log file, the debug shipper, and — when the service
  manager captured standard error, which systemd signals with
  `JOURNAL_STREAM` — the journal as well, so `journalctl -u senhub-agent`
  shows more than the unit starting and stopping.

#### Where the log file is

| OS | Directory | File |
|---|---|---|
| Linux | `/var/log/senhub-agent/` | `senhubagent.log` |
| Windows | `%ProgramData%\SenHub\logs\` | `senhubagent.log` |
| macOS | `/Library/Logs/SenHub/` | `senhubagent.log` |

Two carve-outs, both to stop two processes from truncating one file:

- An **interactive** run writes `senhubagent-console.log` beside the service's
  file, never the file itself.
- A **second instance** — one started with a different `--config-path` — writes
  `senhubagent-<8 hex>.log`, the suffix derived from that path.

If the directory cannot be created or is not writable, the agent falls back to
the directory holding its own binary and says so at startup.

#### Rotation

Rotation is built in; no `logrotate` rule is needed:

| Setting | Value |
|---|---|
| Rotate at | 10 MB |
| Backups kept | 5 |
| Maximum age | 30 days |
| Compression | on (rotated files are gzipped) |

These are not configurable.

#### File format

The file is human-readable text by default. `--log-format json` (or
`SENHUB_LOG_FORMAT=json`) writes one JSON object per line instead, for a log
collector. The console always uses the readable form; the debug shipper always
sends JSON.

#### Redaction

Every writer — file, console, shipper — passes through a masking writer, so a
password or token that reaches a log line is masked before it is written.

### Log Level Behavior

**IMPORTANT**: Info/Warn/Error messages are **ALWAYS** output for all modules, regardless of debug settings. Only Debug logs are filtered by module.

### Verbose mode (`--verbose`)
- **Global level**: DEBUG
- **Modules**: All at DEBUG level
- **Info/Warn/Error**: ALL modules (always visible)
- **Debug**: ALL modules (enabled)
- **Result**: All logs from all components are visible

### Selective mode (`--filter "module1,module2"`)
- **Global level**: INFO (for non-module logs)
- **Specified modules**: DEBUG
- **Non-specified modules**: INFO
- **Info/Warn/Error**: ALL modules (always visible)
- **Debug**: Only specified modules
- **Result**: Info/Warn/Error from all modules + Debug only from specified modules

### Practical Examples

#### Debug only HTTP cache issues
```bash
senhub-agent run --filter "strategy.http"
```

#### Debug every probe at once
```bash
senhub-agent run --filter "probe"
```

#### Debug Windows performance counters (PDH)
```bash
senhub-agent run --filter "pdh.windows"
```

#### Runtime level changes
```bash
# Enable debug for one probe
curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels":[{"module":"probe.postgresql","level":"debug"}]}'

# Disable all logs from a module
curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -H "Content-Type: application/json" \
  -d '{"module_levels":[{"module":"probe.host","level":"disabled"}]}'
```

## When the configuration is not watched

The agent watches its configuration so an edit applies without a
restart. That watch can fail to start for reasons unrelated to the
configuration: on Linux inotify has a per-user instance quota, and a
host running k3s can hold most of it. The agent then keeps collecting
without the watch rather than exiting (from 0.5.5), and says so in three
places:

- a WARN at start, `configuration watch disabled (<reason>): <detail>`;
- a block in `senhub-agent status`;
- the self-metric `senhub.agent.config.watch.disabled{reason}`
  (`senhub_agent_config_watch_disabled` on Prometheus), set to 1 while
  the watch is off. No series means the configuration is watched.

| `reason` | Meaning |
|---|---|
| `watcher_unavailable` | The kernel refused a watcher: the inotify instance quota, or a file system that does not support it |
| `path_not_watched` | The watcher exists but a directory could not be added to it |

The consequence for the operator: **an edit no longer applies until the
service is restarted**, whether it comes from a file, `config set` or the
web console. Alert on the metric, and raise
`fs.inotify.max_user_instances` on hosts that run many watchers.

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

senhub-agent run --filter "strategy.http"

# Or via API, without restarting the service:
curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -d '{"module_levels":[{"module":"strategy.http","level":"debug"}]}'
```

### Scenario 2: Diagnose one probe
```bash
# Problem: a probe is not collecting metrics
# Solution: enable debug for that probe only

senhub-agent run --filter "probe.postgresql"
```

### Scenario 3: Reduce log noise in production
```bash
# Problem: Too many logs in production
# Solution: Reduce log levels via API

curl -X POST http://localhost:8080/api/mykey/debug/logs \
  -d '{
    "module_levels": [
      {"module": "probe.host", "level": "error"},
      {"module": "probe.logicaldisk", "level": "error"},
      {"module": "strategy.http", "level": "warn"}
    ]
  }'
```

## Testing and Validation

### Verify the system works
```bash
# 1. Start the agent with one module selected
senhub-agent run --filter "strategy.http"

# 2. Verify only strategy.http debug logs appear
# 3. Other components still show info/warn/error

# 4. Read back the overrides in force
curl http://localhost:8080/api/{agentkey}/debug/logs
```

### Module Naming Convention

Modules follow a hierarchical convention:
- **Top-level**: `probe`, `strategy`, `configuration`, `status`
- **Sub-modules**: `probe.host`, `strategy.http`, `configuration.local`

This convention enables granular filtering and logical organization of logs.

## Implementation Notes

### Which modules a build actually has

Do not rely on a list written down here — probes and outputs come and go, and
the enterprise build carries modules the OSS build does not. Ask the binary:

```bash
senhub-agent debug-modules-list
```

### Parameter Naming

To avoid conflicts between the `logger` package and `logger` parameter names, constructors use `baseLogger` as the parameter name:

```go
func NewProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
    moduleLogger := logger.NewModuleLogger(baseLogger, "probe.example")
    // ...
}
```

This prevents Go compiler ambiguity between package and variable names.