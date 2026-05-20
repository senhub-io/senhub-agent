# Current Development

This document tracks active development work, completed features, and the roadmap for SenHub Agent.

## Active

### Zabbix native export — paused after audit

**Branch**: `feat/zabbix-export`
**Status**: 🟡 Phase 0 audit complete, implementation paused

Spec drafted on 2026-05-04, audited against current code state on
2026-05-12 (`docs/developer-guide/zabbix/AUDIT-Phase0.md`). The
audit identifies three decisions to take before Phase 1 starts and
estimates implementation at ~5 days. Resuming subject to other
priorities.

## Recently Completed

### Grafana dashboard catalog v1 (0.1.90-beta)
**Status**: ✅ COMPLETED — 21 dashboards live

- 7 Linux + 5 Windows host dashboards
- 1 agent self-monitoring dashboard
- 8 vendor dashboards (Veeam, Redfish, NetScaler, Citrix VDI)
  schema-validated, "(awaiting live data)" annotation drops on
  first customer pilot
- Calqued on the Grafana Cloud Integrations layout (survey +
  proposal under `docs/grafana/research/`)
- Validated live on sha901-prod, sha501-prod (Linux), bbcloud-prod
  (Windows Server 2022)

### Agent self-observability metrics (0.1.90-beta)
**Status**: ✅ COMPLETED

6 process resource metrics + 5 OTLP push counters + new
`system.processes.count` metric. Cross-OS via build tags. The
`BuildAgentRecords` helper moved into a neutral `agentmetrics`
package and is injected into the OTLP push so OTLP-only
deployments see agent self-metrics in their dashboards.

### Native OTLP/gRPC push export (0.1.89-beta → 0.1.90-beta)
**Status**: ✅ COMPLETED

Strategy `otlp` ships metrics + logs over OTLP/gRPC to any OTel
receiver. Full TLS / mTLS / Bearer authentication. New `linux_logs`
probe (free tier) reads the local systemd journal. `${env:VAR}`
expansion in OTLP storage config matches the OTel collector
syntax. Validated end-to-end against VictoriaMetrics +
VictoriaLogs on sha901.

### Prometheus / VictoriaMetrics exposition (0.1.88-beta)
**Status**: ✅ COMPLETED

`/metrics` endpoint with OTel-aligned naming across all 14 probes.
Standard Prometheus text exposition v0.0.4, validated through
expfmt parser in CI. Live validated against VictoriaMetrics.

### Configuration v1→v2 Remote Migration (0.1.70-beta)
**Status**: ✅ COMPLETED

**Objective**: Fix critical production bug where probes fail when server sends v1 config format

**Implementation**:
- In-memory migration system in RemoteConfiguration
- Migration happens before validation (critical ordering)
- Idempotent design - safe to call multiple times
- Zero-downtime automatic migration
- No backup files created (in-memory only)
- Configuration replicated to disk in v2 format

**Benefits**:
- Fixes "probe type is empty" errors
- Backward compatible with v1 server configs
- Forward compatible with v2 server configs
- No user action required

### Shared Configuration Template (0.1.70-beta)
**Status**: ✅ COMPLETED

**Objective**: Eliminate duplication of probe configuration examples across configuration paths

**Implementation**:
- Created `config_template.go` with shared ProbeExamplesTemplate constant
- Extracted 200+ lines of duplicated probe examples
- Single source of truth for all probe documentation
- Used by LocalConfiguration

**Benefits**:
- Adding new probe = update 1 file only (was 2 files)
- Impossible template divergence
- Easier maintenance
- -207 lines of code duplication eliminated

### Standalone Deployment Implementation
**Status**: ✅ COMPLETED

**Objective**: Zero-configuration local deployment

**Features Implemented**:
- Local Configuration System with YAML-based config
- Automatic agent key generation
- HTTPS/TLS support with auto-generated certificates
- Comprehensive CLI for installation and management
- Local dashboard with system overview and API explorer
- Multiple API formats (PRTG, Nagios, SenHub, Prometheus)
- Certificate management with auto-renewal

**Documentation**: See `/docs/user-guide/` for complete deployment guides

### Universal Configuration API
**Status**: ✅ COMPLETED

**Objective**: Provide universal configuration validation for all probe types

**Features**:
- Three validation levels: schema, connectivity, full
- Universal probe support (Redfish, WebApp, System probes, etc.)
- Monitoring system integration (PRTG, Nagios, Zabbix)
- Comprehensive error handling with detailed feedback
- Preview metrics for verification

**Endpoints**:
- `/config/validate` - Validate configuration structure
- `/config/preview` - Preview configuration with connectivity tests
- `/config/test` - Full validation with metrics preview

**Documentation**: `/docs/admin-guide/UNIVERSAL-CONFIGURATION.md`

### JWT-Based License System
**Status**: ✅ COMPLETED

**Objective**: Implement production-ready license validation using RSA-signed JWT tokens

**Implementation**:
- RSA-4096 asymmetric cryptography for signature verification
- JWT token validation with claims: tier, authorized_probes, exp, iat, iss, sub
- Three license tiers: Free (basic probes), Pro (advanced probes), Enterprise (all probes)
- Grace period handling (7 days after expiration)
- HTTP API endpoint for license status inspection
- Production key generation tools for Sensor Factory

**Security**:
- Public key embedded in agent binary (`internal/agent/services/license/public_key.go`)
- Private key stored securely in Sensor Factory vault (never in agent)
- Token validation on agent startup and API access
- License data stored in agent configuration (`agent.license` field)

**Files**:
- `/internal/agent/services/license/` - License validation service
- `/scripts/license-generator/` - Production license generation tool (for Sensor Factory)
- `/scripts/generate-keys/` - Development test key generation
- `.keys/production/` - Production RSA key pair (private key excluded from Git)

**Benefits**:
- Flexible probe authorization per customer
- Offline license validation (no phone-home required)
- Tamper-proof license tokens
- Easy integration with Sensor Factory backend
- Graceful degradation with clear error messages

**API Endpoints**:
- `GET /api/{key}/license/status` - View license tier, expiration, authorized probes
- Web UI displays license information in dashboard

**Documentation**: `/docs/LICENSE-SYSTEM.md`

## Active Development

### Redfish Probe
**Status**: 🔨 IN PROGRESS

**Objective**: Port Python Redfish monitoring plugin to Go probe with vendor-specific collectors

**Completed**:
- ✅ Core probe structure and generic collector
- ✅ Redfish API client with session handling
- ✅ Vendor detection logic
- ✅ Vendor-specific collectors (Dell, HPE, Lenovo, Cisco)
- ✅ Storage collector for Dell PowerVault ME5024
- ✅ Probe registration in registry
- ✅ Collection-specific metrics (system, thermal, power, processor, memory, storage, network)
- ✅ Comprehensive unit and integration tests
- ✅ Documentation in REDFISH-METRICS.md
- ✅ Storage metrics for health, capacity, and performance
- ✅ Disk operation tracking (rebuilds, formatting, etc.)

**TODO**:
1. Optimize caching system for performance
2. Add support for additional vendors (SuperMicro, Fujitsu, etc.)
3. Extend metrics for additional storage operations

**Files**:
- `/internal/agent/probes/redfish/redfishProbe.go`
- `/internal/agent/probes/redfish/collector_*.go`
- `/internal/agent/probes/redfish/redfish_client.go`

### Citrix Probe
**Status**: 🔨 IN PROGRESS

**Objective**: Monitor Citrix Virtual Apps and Desktops (CVAD) environments

**Architecture**:
- OData API (Director): Real-time session, machine, connection metrics
- DDC REST API: Site inventory, filtering, delivery groups

**Completed Metrics**:
- ✅ Connected/disconnected sessions
- ✅ Infrastructure (machines registered/unregistered/faulty)
- ✅ Connection failures by category
- ✅ UX scores (Excellent/Good/Fair/Poor)
- ✅ Health score calculation

**Active Issue**: Logon duration discrepancy
- **Director**: 18-19 seconds (1h average)
- **Agent**: 11.85 seconds
- **Gap**: ~6-7 seconds

**Investigation Status**:
- ✅ Reconnections excluded (matches Director)
- ✅ 1h sliding window aligned
- ✅ HDX protocol only
- ✅ Complete sessions only (LogOnEndDate != null)

**Hypotheses**:
1. Director might use `Session.LogOnDuration` directly instead of calculating from Connections
2. Phase overlap handling might differ
3. Additional undocumented filtering criteria

**Next Steps**:
1. Test using `Session.LogOnDuration` field directly
2. Analyze phase overlap handling
3. Compare raw API responses with Director UI

**Configuration**:
```yaml
- name: citrix
  params:
    base_url: "https://director.noble-age.fr"
    delivery_controller:
      url: "https://SW000-209-030.noble-age.fr"
      fallback_urls:
        - "https://SW000-209-031.noble-age.fr"
      site_filter: "PROD"
    interval: 120
```

**Files**:
- `/internal/agent/probes/citrix/citrixProbe.go`
- `/internal/agent/probes/citrix/metrics_*.go`
- `/internal/agent/probes/citrix/citrix_client.go`

### Windows Event Log Probe
**Status**: 🚧 EARLY STAGE

**Objective**: Create a probe to collect Windows Event Log entries

**Completed**:
- ✅ Core probe structure with Windows-specific implementation
- ✅ Event query builder with filters
- ✅ Probe registration as "winevents"
- ✅ Basic tests for configuration parsing

**TODO**:
1. Complete Windows API integration with event subscription
2. Add proper event XML parsing for all fields
3. Implement efficient event filtering
4. Add integration tests with Windows event log
5. Optimize performance for high-volume event logs

**Files**:
- `/internal/agent/probes/event/winevents/wineventsProbe.go`
- `/internal/agent/probes/event/winevents/wineventsProbe_windows.go`

### HTTP Strategy
**Status**: ✅ PRODUCTION READY

**Objective**: Expose agent metrics via HTTP REST API for external monitoring tools

**Features**:
- HTTP sync strategy with gorilla/mux router
- POST endpoint `/api/{agentkey}/prtg/metrics` for PRTG integration
- Metric caching system with TTL and automatic cleanup
- Modular transformer system for user-friendly metric names
- Configurable bind address support
- Authentication via agent key in URL path
- PRTG JSON response format
- Health check endpoint
- Graceful shutdown

**Configuration Example**:
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

**TODO**:
1. Add support for GET endpoints for other monitoring tools
2. Implement dynamic configuration updates from POST body
3. Add support for additional transformer patterns
4. Add Prometheus format support

**Files**:
- `/internal/agent/services/data_store/strategy_http.go`
- `/internal/agent/services/data_store/transformers/transformer.go`

### Modular Logging System
**Status**: ✅ PRODUCTION READY

**Objective**: Implement granular log level control per module/component

**Features**:
- Module-based logging system with configurable levels
- HTTP endpoints for runtime log level management
- Logger filtering at module level
- 16 predefined modules with individual log levels
- CLI support: `--verbose` and `--debug-modules`

**Modules**:
- `strategy.http`, `strategy.prtg`, `strategy.senhub`
- `probe.redfish`, `probe.host`, `probe.network`, `probe.webapp`, `probe.gateway`, `probe.syslog`
- `cache`, `transformer`, `scheduler`, `configuration`

**API Endpoints**:
- `GET /api/{agentkey}/debug/logs` - View current module log levels
- `POST /api/{agentkey}/debug/logs` - Set module log levels dynamically

**Usage**:
```bash
# View current log levels
curl http://localhost:8080/api/YOUR_AGENT_KEY/debug/logs

# Enable debug for specific modules
./agent run --verbose --debug-modules strategy.http,cache
```

**TODO**:
1. Add configuration file support for default module log levels
2. Implement log level inheritance for sub-modules

## Roadmap

### Short Term (Next Sprint)
- Complete Citrix logon duration investigation
- Complete Windows Event Log probe implementation
- Add Prometheus format support to HTTP strategy
- Optimize Redfish probe caching

### Medium Term (Next Quarter)
- Add support for additional Redfish vendors
- Implement hybrid mode (online/offline auto-switch)
- Add clustering support for offline agents
- Enhance web interface with configuration editor

### Long Term (Future)
- Agent-to-agent communication
- Distributed monitoring coordination
- Advanced analytics and alerting
- Plugin system for custom probes

## Known Issues

### Windows Service Configuration
**Issue**: Port doesn't bind when running as Windows service

**Status**: Under investigation

**Cause**: Service working directory differs from expected path

**Workaround**:
```powershell
# Reinstall with absolute config path
senhub-agent.exe install --config-path "C:\Program Files\Senhub\Senhub Agent\agent.yaml"
```

## Development Session Info

**Work Directory**: `/Users/matthieu/Documents/GitHub/senhub-agent/`

**Recent Commits**:
- `409d573` - feat(sensor): add duplicate probe name detection and validation
- `53f06ce` - feat(cache): implement discriminant tags system for stable time series keys
- `f4bec6f` - fix(citrix): exclude reconnections from logon duration calculation
- `4fa1538` - fix(citrix): align logon duration calculation with Director console

**Dependencies**:
- `github.com/gorilla/mux` - HTTP routing
- `github.com/rs/zerolog` - Structured logging
- `gopkg.in/yaml.v2` - YAML configuration parsing

## Contributing to Current Work

When contributing to active development:
1. Check this document for current status
2. Review related files and tests
3. Follow established patterns in the codebase
4. Add tests for new functionality
5. Update documentation as needed

See [Development Workflow](./development-workflow.md) for process details.

---

Last updated: 2025-12-09
