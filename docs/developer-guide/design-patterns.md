# Design Patterns & Best Practices

This document describes the design patterns and best practices used in SenHub Agent development.

## Core Design Patterns

### 1. Modular Architecture Pattern

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
}
```

**Benefits:**
- Single Responsibility Principle: Each manager handles one concern
- Easier testing and maintenance
- Clear separation of concerns
- Modular development

### 2. Delegation Pattern

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

**When to use:**
- Complex handlers with business logic
- Operations that span multiple concerns
- Logic that needs testing in isolation

### 3. Encapsulation with Controlled Access

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

### 4. Module-Specific Logging

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

### 5. Helper Function Pattern

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

### 6. Configuration Provider Pattern

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

**Benefits:**
- Unified interface for different config sources
- Easy testing with mock providers
- Flexible deployment modes

### 7. Interface-Based Design

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

**Principles:**
- Small, focused interfaces
- Composable through embedding
- Easy to mock for testing

### 8. Error Handling Pattern

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

**Guidelines:**
- Always wrap errors with context using `fmt.Errorf` and `%w`
- Log errors with relevant structured fields
- Use appropriate log levels (Error, Warn, Info, Debug)
- Include service/component name in logs

## Testing Patterns

### Table-Driven Tests

```go
func TestMetricTransformation(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"CPU temp", "thermal.cpu.0.temperature", "CPU Temperature - Processor 0"},
        {"Memory usage", "system.memory.usage", "Memory Usage"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := transform(tt.input)
            if result != tt.expected {
                t.Errorf("got %v, want %v", result, tt.expected)
            }
        })
    }
}
```

### Mock Interfaces

```go
type MockProbe struct {
    CollectCalled bool
    DataPoints    []DataPoint
}

func (m *MockProbe) Collect() error {
    m.CollectCalled = true
    return nil
}
```

### Integration Tests

```go
func TestHTTPEndpoint(t *testing.T) {
    server := setupTestServer()
    defer server.Close()

    resp, err := http.Get(server.URL + "/api/test/metrics")
    if err != nil {
        t.Fatal(err)
    }

    if resp.StatusCode != http.StatusOK {
        t.Errorf("expected 200, got %d", resp.StatusCode)
    }
}
```

### Benchmark Tests

```go
func BenchmarkMetricTransformation(b *testing.B) {
    transformer := NewTransformer()

    for i := 0; i < b.N; i++ {
        transformer.Transform("thermal.cpu.0.temperature")
    }
}
```

## Code Organization Rules

### 1. File Naming
Use descriptive names that indicate purpose:
- `http_web.go` - Web interface handlers
- `http_api.go` - API endpoint handlers
- `http_auth.go` - Authentication logic
- `redfish_client.go` - Redfish API client
- `citrix_metrics.go` - Citrix metrics collection

### 2. Function Ordering
Within each file:
1. Public functions (exported)
2. Private helper functions (unexported)
3. Structured in logical flow

### 3. Import Grouping
```go
import (
    // Standard library
    "context"
    "fmt"
    "time"

    // Third-party packages
    "github.com/gorilla/mux"
    "github.com/rs/zerolog"

    // Internal packages
    "senhub-agent/internal/agent/probes/types"
)
```

### 4. Comment Structure
```go
// Package cpu implements CPU monitoring probe.
// It collects metrics for CPU usage, load, and temperature.
package cpu

// Collect gathers CPU metrics from the system.
// It returns an error if the system call fails.
func (p *cpuProbe) Collect() error {
    // Implementation...
}
```

### 5. Manager Initialization
Create all managers in constructor, initialize in order of dependencies:

```go
func NewHTTPSyncStrategy(...) *HTTPSyncStrategy {
    // Create all managers
    authManager := NewAuthenticationManager(...)
    healthManager := NewHealthManager(...)
    apiManager := NewAPIManager(...)

    return &HTTPSyncStrategy{
        authManager:   authManager,
        healthManager: healthManager,
        apiManager:    apiManager,
    }
}
```

## Pattern Compliance Checklist

Before committing new code, verify compliance with our patterns:

### Modular Architecture
- [ ] New functionality added to appropriate manager (not HTTPSyncStrategy directly)
- [ ] Manager follows single responsibility principle
- [ ] Manager initialized in NewHTTPSyncStrategy constructor
- [ ] Manager has proper encapsulation getter: `GetXxxManager()`

### Delegation Pattern
- [ ] HTTPSyncStrategy handlers delegate to managers: `h.apiManager.HandleXxx(w, r)`
- [ ] No business logic in main strategy handlers (only delegation)
- [ ] Comments indicate delegation: `// (delegated to XxxManager)`

### Helper Functions
- [ ] Common operations extracted to helper functions
- [ ] Helper functions are pure (no side effects when possible)
- [ ] Helper functions have descriptive names
- [ ] Complex helpers documented with examples

### Logging
- [ ] Module-specific logger used: `logger.NewModuleLogger(baseLogger, "module.name")`
- [ ] Structured logging with relevant fields
- [ ] Error logging includes context
- [ ] Debug/Info messages provide meaningful information

### HTTP Headers
- [ ] Dynamic HTML pages use `setNoCacheHeaders()`
- [ ] Static assets use appropriate cache headers
- [ ] JSON APIs have consistent headers

### Error Handling
- [ ] Errors wrapped with context: `fmt.Errorf("operation failed: %w", err)`
- [ ] Errors logged with structured fields
- [ ] HTTP errors use appropriate status codes
- [ ] Resource cleanup in error paths

### Comments & Documentation
- [ ] Public functions have descriptive comments
- [ ] Getters commented as "(read-only access)"
- [ ] Complex logic documented with inline comments
- [ ] Interface implementations documented

### Testing
- [ ] Unit tests for new functionality
- [ ] Tests updated for modified behavior
- [ ] Integration tests for HTTP endpoints
- [ ] All tests pass: `make test`

## Anti-Patterns to Avoid

### ❌ God Objects
Don't create objects that do everything:
```go
// Bad: Single object handles everything
type Agent struct {
    // 50+ fields
    // 100+ methods
}
```

### ❌ Circular Dependencies
Avoid import cycles:
```go
// Bad: package A imports B, B imports A
```

### ❌ Hardcoded Values
Use constants or configuration:
```go
// Bad
timeout := 30

// Good
const DefaultTimeout = 30 * time.Second
timeout := config.GetTimeout()
```

### ❌ Silent Errors
Always handle errors properly:
```go
// Bad
service.Start()

// Good
if err := service.Start(); err != nil {
    return fmt.Errorf("failed to start service: %w", err)
}
```

### ❌ Copy-Paste Code
Extract common logic to shared functions:
```go
// Bad: Duplicated code in multiple places

// Good: Shared helper function
func validateConfig(cfg Config) error {
    // Validation logic
}
```

## Best Practices Summary

1. **Modular Architecture**: Split large components into focused managers
2. **Delegation**: Delegate to specialized components
3. **Encapsulation**: Provide controlled access through getters
4. **Logging**: Use module-specific loggers for targeted debugging
5. **Error Handling**: Add context to all errors
6. **Testing**: Write tests for all new functionality
7. **Documentation**: Comment public APIs and complex logic
8. **Code Organization**: Follow consistent file and function ordering
9. **Interfaces**: Define clear interfaces for extensibility
10. **Resource Management**: Implement proper lifecycle management

## Next Steps

- Review [Architecture](./architecture.md) for system design
- Check [Build System](./build-system.md) for testing procedures
- See [Current Development](./current-development.md) for active work

---

Last updated: 2025-11-06
