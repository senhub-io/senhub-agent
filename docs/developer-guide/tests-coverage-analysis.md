# Test coverage analysis — SenHub Agent

**Snapshot date:** 2025-10-14
**Status:** a dated snapshot, kept as the record of where the coverage gaps
were when the analysis was run. The figures below have not been refreshed
since; several of the packages named here have been renamed, split or
removed. Re-measure before planning work off it.

## Tests fixed

- **TestDetectAgentMode**: fixed to match the new behaviour (the config key
  wins in offline mode)
- **New test**: added for online mode with a key mismatch

## Overall state

**All tests passing.**

- 39 packages tested
- Overall coverage: ~32%

## Packages by priority of missing tests

### Critical — 0% coverage (no tests at all)

#### 1. `cliArgs` (0%)

**Impact**: critical — CLI argument parsing
**Files**: `internal/agent/cliArgs/cliArgs.go`
**Missing tests**:
- CLI argument parsing
- Flag validation
- Default values
- Parse-error handling

#### 2. `formats/event` (0%)

**Impact**: high — event format
**Files**: `internal/agent/formats/event/*.go`
**Missing tests**:
- JSON serialisation/deserialisation
- Event format validation
- Event transformation

#### 3. `probes` (registry) (0%)

**Impact**: critical — the probe registry
**Files**: `internal/agent/probes/registry.go`
**Missing tests**:
- Probe registration
- Lookup by type
- Name-collision handling
- Listing the available probes

#### 4. `services/data_store` (core) (0%)

**Impact**: critical — the heart of the data store
**Files**: `internal/agent/services/data_store/data_store.go`
**Missing tests**:
- Data-store initialisation
- Routing to strategies
- Routing-error handling
- Clean shutdown

#### 5. `services/sensor` (0%)

**Impact**: critical — probe orchestration
**Files**: `internal/agent/services/sensor/*.go`
**Missing tests**:
- Probe lifecycle
- Periodic collection
- Callback handling
- Error recovery

#### 6. `services/server` (0%)

**Impact**: critical — remote configuration
**Files**: `internal/agent/services/server/*.go`
**Missing tests**:
- Fetching the server config
- Parsing API responses
- Timeout and network-error handling
- Retry logic

#### 7. `strategies/event` (0%)

**Impact**: medium — the event strategy
**Files**: `internal/agent/services/data_store/strategies/event/*.go`
**Missing tests**:
- Event storage
- Buffering/batching
- Overflow handling

#### 8. `strategies/senhub` (0%)

**Impact**: high — the SenHub platform strategy
**Files**: `internal/agent/services/data_store/strategies/senhub/*.go`
**Missing tests**:
- Sending data to SenHub
- Authentication
- Retry/backoff
- Compression

#### 9. `services/status` (0%)

**Impact**: low — status reporting
**Files**: `internal/agent/services/status/*.go`
**Missing tests**:
- Health checks
- Status aggregation
- Agent metrics

#### 10. `types/event` (0%)

**Impact**: medium — event structures
**Files**: `internal/agent/types/event/*.go`
**Missing tests**:
- Structure validation
- Serialisation
- Construction helpers

### Priority — insufficient coverage (<20%)

#### 1. `gateway` probe (1.4%)

**Files**: `internal/agent/probes/gateway/*.go`
**Existing tests**: basic only
**Missing tests**:
- Ping timeout
- Packet-loss calculation
- Multiple gateways
- IPv4/IPv6
- Network errors

#### 2. `host` probe (1.6%)

**Files**: `internal/agent/probes/host/*.go`
**Existing tests**: minimal
**Missing tests**:
- Collecting every metric (CPU, RAM, disk, network)
- Cross-platform (Windows/Linux/macOS)
- WMI/procfs error handling
- Performance (no memory leaks)

#### 3. `logicaldisk` probe (6.2%)

**Files**: `internal/agent/probes/logicaldisk/*.go`
**Missing tests**:
- Every filesystem
- Network drives
- IOPS metrics
- Mount-point handling

#### 4. `network` probe (10.2%)

**Files**: `internal/agent/probes/network/*.go`
**Missing tests**:
- Every interface (ethernet, wifi, VPN)
- Detailed metrics (packets, errors)
- Virtual-interface handling
- Interface hot-plug

#### 5. `cpu` probe (12.0%)

**Files**: `internal/agent/probes/cpu/*.go`
**Missing tests**:
- Multi-core metrics
- Load average
- CPU frequency
- Cross-platform

#### 6. `citrix` probe (12.1%)

**Files**: `internal/agent/probes/citrix/*.go`
**Existing tests**: basic
**Critical missing tests**:
- **Complete site filtering**
- **Logon duration calculation** (an 11s vs 18s discrepancy to investigate)
- DDC fallback
- Session metrics
- Connection failures
- OData filtering

#### 7. `webapp` probe (13.5%)

**Files**: `internal/agent/probes/webapp/*.go`
**Missing tests**:
- HTTP/HTTPS requests
- Timeouts
- Response validation
- TLS verification
- Redirects

#### 8. `memory` probe (14.8%)

**Files**: `internal/agent/probes/memory/*.go`
**Missing tests**:
- RAM vs swap
- Cache metrics
- Cross-platform

#### 9. `redfish` probe (20.7%)

**Files**: `internal/agent/probes/redfish/*.go`
**Existing tests**: basic structure
**Missing tests**:
- Every vendor (Dell, HPE, Lenovo, Cisco)
- Every collector (thermal, power, storage)
- Authentication
- Session management
- Error handling

### Acceptable — medium coverage (20-50%)

#### 1. `syslog` probe (27.4%)

**To improve**:
- Complete RFC 5424 parsing
- UDP vs TCP
- Multi-facility

#### 2. `http` strategy (32.4%)

**Existing tests**: good
**To improve**:
- Complete TLS/HTTPS
- Every endpoint (PRTG, Nagios, Prometheus)
- Configuration validation API
- Authentication edge cases

#### 3. `agent` core (37.3%)

**To improve**:
- Service orchestration
- Graceful shutdown
- Error recovery

#### 4. `auto_update` (52.5%)

**To improve**:
- Download retry
- Signature verification
- Rollback

#### 5. `configuration` service (53.7%)

**To improve**:
- Migration edge cases
- File watching
- Hot reload

### Well tested — high coverage (>75%)

- **configParser**: 100%
- **validators**: 91.7%
- **debugshipper**: 87.5%
- **periodic_scheduler**: 85.7%
- **transformers**: 77.5%
- **prtg strategy**: 77.1%

## Recommendations, in priority order

### Phase 1 — missing critical tests (1-2 days)

1. **cliArgs**: CLI parsing tests (2h)
2. **probes/registry**: registry tests (2h)
3. **services/data_store**: core data-store tests (4h)
4. **services/sensor**: probe-orchestration tests (4h)

### Phase 2 — the main probes (2-3 days)

5. **host probe**: raise from 1.6% → 60% (6h)
6. **gateway probe**: raise from 1.4% → 50% (4h)
7. **citrix probe**: raise from 12.1% → 60% (8h) plus the logon-duration fix
8. **redfish probe**: raise from 20.7% → 50% (6h)

### Phase 3 — strategies and services (1-2 days)

9. **strategies/senhub**: write tests (4h)
10. **services/server**: remote-config tests (4h)
11. **http strategy**: complete coverage → 60% (4h)

### Phase 4 — remaining tests (1 day)

12. The remaining probes (cpu, memory, network, disk, webapp) → 50%
13. services/status
14. formats/event

## Total estimate: 6-8 days of development

## Useful commands

The test suite itself is run through the Makefile — `make test` — which sets
the locale, log directories, `-ldflags` injection and CGO environment that a
raw `go test` skips. The `go` commands below are for producing a coverage
profile from an already-working environment, not for running the suite.

### Coverage profile

```bash
go test ./... -coverprofile=coverage.out
```

### Coverage per package

```bash
go test ./... -cover
```

### HTML coverage report

```bash
go tool cover -html=coverage.out -o coverage.html
```

### One package

```bash
go test -v ./internal/agent/probes/citrix -run TestMetricsCollector
```

### Untested functions

```bash
go tool cover -func=coverage.out | grep "0.0%"
```
