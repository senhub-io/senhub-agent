# Build System

This document describes the build system, compilation process, and testing procedures for SenHub Agent.

## Build Commands

The project uses a Makefile for consistent build operations across all platforms.

### Basic Commands

```bash
# Build all binaries (darwin, linux, windows)
make build

# Build for specific OS
make build-darwin    # macOS binary
make build-linux     # Linux binary
make build-windows   # Windows binary

# Development with live reload
make watch

# Clean build artifacts
make clean
```

### Build Output

Binaries are placed in the `dist/` directory:
```
dist/
├── senhub-agent_darwin_amd64
├── senhub-agent_linux_amd64
└── senhub-agent_windows_amd64.exe
```

## Testing

### Test Commands

```bash
# Run all tests
make test

# Run tests with race detection
make test-race

# Run specific test
go test -v ./path/to/package -run TestName

# Run tests with coverage
make test
# View coverage report
open coverage.html
```

### Testing Best Practices

**CRITICAL**: ALWAYS use `make test` instead of running `go test` directly.

Why?
- The Makefile ensures consistent test execution across all environments
- It sets up proper environment variables
- It handles cross-platform differences
- It generates coverage reports

**Test Execution Workflow:**
1. Before committing: `make test` to verify all tests pass
2. For race conditions: `make test-race`
3. For specific tests: `go test -v ./path/to/package -run TestName`
4. Check coverage: Open `coverage.html` in browser

### Test Requirements

When making code changes:
- ✅ **New functionality** → Add new tests
- ✅ **Modified behavior** → Update existing tests
- ✅ **Bug fixes** → Add regression tests
- ✅ **API changes** → Update integration tests
- ✅ **All tests pass** → Run `make test` before committing

### Test Organization

```
internal/
├── agent/
│   ├── probes/
│   │   ├── cpu/
│   │   │   ├── cpuProbe.go
│   │   │   └── cpuProbe_test.go    # Unit tests
│   │   ├── redfish/
│   │   │   ├── redfishProbe.go
│   │   │   └── redfishProbe_test.go
│   │   └── ...
│   └── services/
│       └── data_store/
│           ├── strategy_http.go
│           └── strategy_http_test.go
```

### Test Patterns

#### Table-Driven Tests
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

#### Mock Interfaces
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

#### Integration Tests
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

## Code Style

### Formatting
- Use `gofmt` for consistent formatting
- Enforced by pre-commit hook
- No need to manually format - hook handles it

### Import Organization
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
    "senhub-agent/internal/agent/services/data_store"
)
```

### Naming Conventions
- **PascalCase**: Exported identifiers (functions, types, constants)
- **camelCase**: Unexported identifiers
- **ALL_CAPS**: Constants (only when necessary)

Examples:
```go
// Exported
type ProbeConfig struct { ... }
func NewProbe() *Probe { ... }
const DefaultTimeout = 30

// Unexported
func processMetrics() { ... }
var clientTimeout = 30
```

### Error Handling
```go
// ✅ Good: Add context to errors
if err := service.Start(); err != nil {
    return fmt.Errorf("failed to start HTTP server: %w", err)
}

// ✅ Good: Log errors with structured fields
logger.Error().
    Err(err).
    Str("service", serviceName).
    Msg("Failed to start service")

// ❌ Bad: Silent errors
service.Start()

// ❌ Bad: Generic error messages
if err := service.Start(); err != nil {
    return err
}
```

### Comments
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

### Cross-Platform Code

Use build tags for platform-specific code:

```go
// file: process_unix.go
//go:build !windows
// +build !windows

package process

func getProcessInfo() (*ProcessInfo, error) {
    // Unix implementation
}
```

```go
// file: process_windows.go
//go:build windows
// +build windows

package process

func getProcessInfo() (*ProcessInfo, error) {
    // Windows implementation
}
```

## Dependencies

### Adding Dependencies
```bash
# Add a new dependency
go get github.com/example/package

# Update go.mod and go.sum
go mod tidy

# Verify dependencies
go mod verify
```

### Current Dependencies
- `github.com/gorilla/mux` - HTTP routing
- `github.com/rs/zerolog` - Structured logging
- `gopkg.in/yaml.v2` - YAML configuration parsing

See `go.mod` for complete list.

## Build Configuration

### GoReleaser
The project uses GoReleaser for automated releases:
- Configuration: `goreleaser.yaml`
- Triggered by: Git tags on `dev` branch
- Outputs: Binaries for darwin, linux, windows

### GitHub Actions
Automated workflows:
- **Test**: Run tests on push/PR
- **Beta Release**: Create beta release on dev branch push
- **Production Release**: Create production release on master tag

## Performance

### Profiling
```bash
# CPU profiling
go test -cpuprofile cpu.prof -bench .

# Memory profiling
go test -memprofile mem.prof -bench .

# View profile
go tool pprof cpu.prof
```

### Benchmarks
```go
func BenchmarkMetricTransformation(b *testing.B) {
    transformer := NewTransformer()

    for i := 0; i < b.N; i++ {
        transformer.Transform("thermal.cpu.0.temperature")
    }
}
```

Run benchmarks:
```bash
go test -bench=. ./internal/agent/services/data_store/transformers/
```

## Troubleshooting

### Common Build Issues

**Issue**: Import cycle detected
```
Solution: Refactor packages to remove circular dependencies
```

**Issue**: Tests fail on Windows but pass on Mac
```
Solution: Check for platform-specific code, use build tags
```

**Issue**: Coverage drops unexpectedly
```
Solution: Run `make test` and check coverage.html for untested paths
```

### Debug Build
```bash
# Build with debug symbols
go build -gcflags="all=-N -l" -o dist/agent-debug cmd/agent/main.go

# Run with debugger
dlv exec dist/agent-debug
```

## Next Steps

- Review [Design Patterns](./design-patterns.md) for code organization
- Check [Development Workflow](./development-workflow.md) for Git process
- See [Current Development](./current-development.md) for active work

---

Last updated: 2025-11-06
