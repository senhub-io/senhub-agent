# SenHub Agent Development Guidelines

This file serves as the primary development guide reference for Claude Code and human developers.

## Complete Developer Documentation

The full development documentation has been moved to `/docs/developer-guide/` for better organization:

- **[Developer Guide Home](./docs/developer-guide/README.md)** - Start here
- **[Architecture](./docs/developer-guide/architecture.md)** - System design and patterns
- **[Development Workflow](./docs/developer-guide/development-workflow.md)** - Git, branches, releases
- **[Build System](./docs/developer-guide/build-system.md)** - Makefile, compilation, testing
- **[Design Patterns](./docs/developer-guide/design-patterns.md)** - Code patterns and best practices
- **[Current Development](./docs/developer-guide/current-development.md)** - Active work and roadmap

## Quick Reference

### Critical Rules (READ FIRST)
- **NO automatic pushes** to remote repositories
- **NO beta releases** until user approval
- **NO commits directly to dev** - always use feature branches
- **ALWAYS use `make test`** instead of running `go test` directly
- **Feature branches first** - merge to dev only when sufficiently tested

### ⚠️ Temporary Dependencies Forks

**IMPORTANT**: We maintain temporary forks of upstream dependencies with critical bug fixes:

- **citrix/adc-nitro-go** → `senhub-io/adc-nitro-go` (singleton stats bug)
  - **Why**: Fixes panic on system/ns/ssl metrics (issue #35, 3+ years old)
  - **Fix**: FindAllStats() + FindStat() for singleton resources
  - **Doc**: `docs/.internal/TEMPORARY-FORK-citrix-adc-nitro-go.md`
  - **Review**: Quarterly (next: 2025-03-11)
  - **Revert when**: Upstream merges PR #36

See `docs/.internal/TEMPORARY-FORK-*.md` for all active forks and revert procedures.

### Version Management
- **Production version**: Without `-beta` suffix (e.g., `0.1.64`)
- **Development version**: With `-beta` suffix (e.g., `0.1.70-beta`)
- **Tag format**: `X.Y.Z-beta` (NO "v" prefix)

### Branch Strategy
```bash
# 1. Create feature branch
git checkout -b feature/my-feature-name

# 2. Develop and test locally
make build-darwin && make test
git commit -m "feat: add new feature"

# 3. Merge to dev (LOCAL)
git checkout dev
git merge feature/my-feature-name

# 4. Push to remote (ONLY with user approval)
git push origin dev
```

### Build Commands
```bash
make build              # Build all binaries
make build-darwin       # Build for macOS
make build-windows      # Build for Windows
make build-linux        # Build for Linux
make test               # Run all tests (ALWAYS use this)
make test-race          # Run tests with race detection
make clean              # Clean build artifacts
```

### Testing Requirements
- **New functionality** → Add new tests
- **Modified behavior** → Update existing tests
- **Bug fixes** → Add regression tests
- **Before committing** → Run `make test`

### Code Style
- **Formatting**: gofmt (enforced by pre-commit hook)
- **Imports**: Standard library, third-party, internal (with blank lines between)
- **Naming**: PascalCase (exported), camelCase (unexported)
- **Error handling**: Add context with `fmt.Errorf("message: %w", err)`

### Project Architecture
```
Probes → DataStore → Strategies
         ↑
    Configuration Provider (Remote/Local)
```

- **Probes**: Collect metrics (embed BaseProbe)
- **DataStore**: Route data to strategies
- **Strategies**: senhub, prtg, event, http
- **Configuration**: Remote (online) or Local (offline)

### Configuration Format (v2)
```yaml
probes:
  - name: Production Citrix    # Display name (free choice)
    type: citrix               # Probe type (must match registry)
    params:
      base_url: "https://..."
      interval: 120
```

**Key distinction**:
- `name`: Unique instance identifier (cache keys, metrics tags)
- `type`: Technical probe class (registry, transformers, discriminant tags)

### Debugging
```bash
# Full debug mode
./agent run --authentication-key KEY --verbose

# Selective debug mode
./agent run --authentication-key KEY --verbose --debug-modules strategy.http,cache

# Runtime log level changes
curl -X POST http://localhost:8080/api/{key}/debug/logs \
  -d '{"module_levels": [{"module": "probe.redfish", "level": "debug"}]}'
```

### License System
```yaml
# Configuration (agent.license field)
agent:
  authentication_key: "agent-uuid"
  license: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."  # JWT token

# CLI: Check license status
curl http://localhost:8080/api/{key}/license/status

# Response includes:
# - tier: "Free" | "Pro" | "Enterprise"
# - expires_at: ISO 8601 timestamp
# - authorized_probes: ["cpu", "memory", ...]
# - grace_period: boolean (7 days after expiration)
```

**License Tiers:**
- **Free**: cpu, memory, logicaldisk, network
- **Pro**: redfish, citrix, ping, snmp, syslog, event
- **Enterprise**: all probes (wildcard)

**Key Files:**
- `/internal/agent/services/license/` - License validation
- `/scripts/license-generator/` - Production license tool (Sensor Factory)
- `/docs/LICENSE-SYSTEM.md` - Complete documentation

### Design Patterns Checklist
Before committing:
- [ ] Module-specific logger used
- [ ] Errors wrapped with context
- [ ] Tests updated/added
- [ ] Follows established patterns (BaseProbe, delegation, etc.)
- [ ] Public functions documented
- [ ] Resource cleanup implemented

### Code Review Checklist
- [ ] Tests updated for ALL code changes
- [ ] Tests actually pass
- [ ] Error handling with context
- [ ] Follows architecture patterns
- [ ] Documentation updated

### Adding New Components

**New Probe**:
1. Implement `types.Probe` interface
2. Embed `types.BaseProbe`
3. Register in `probes/registry.go`
4. Add transformer definitions
5. Add comprehensive tests

**New Strategy**:
1. Implement `Strategy` interface
2. Register in `data_store/data_store.go`
3. Add configuration schema
4. Add tests

**New HTTP Endpoint**:
1. Create handler in appropriate manager
2. Register route with authentication
3. Add integration tests

## Work Directory

`/Users/matthieu/Documents/GitHub/senhub-agent/`

## Current Development Status

Active work:
- **Redfish Probe**: Hardware monitoring (in progress)
- **Citrix Probe**: CVAD monitoring (investigating logon duration)
- **Windows Event Log Probe**: Event collection (early stage)
- **HTTP Strategy**: REST API exposure (production ready)
- **Modular Logging**: Per-module log control (production ready)

See [Current Development](./docs/developer-guide/current-development.md) for details.

## Recent Completions

- ✅ Configuration v1→v2 migration (0.1.70-beta)
- ✅ Shared configuration template (0.1.70-beta)
- ✅ Offline mode implementation
- ✅ Universal configuration API
- ✅ Modular logging system

## Documentation Structure

- `/docs/user-guide/` - End-user documentation
- `/docs/admin-guide/` - Administration and configuration
- `/docs/probes/` - Probe-specific documentation
- `/docs/developer-guide/` - This expanded guide
- `/docs/releases/` - Release notes and changelog
- `/docs/.internal/` - Internal documentation and tooling

For complete documentation, see [Documentation Index](./docs/README.md).

## Dependencies

- `github.com/gorilla/mux` - HTTP routing
- `github.com/rs/zerolog` - Structured logging
- `gopkg.in/yaml.v2` - YAML configuration parsing

See `go.mod` for complete list.

## Git Commit Guidelines

**DO NOT** add:
- "Co-Authored-By: Claude" signatures
- "Generated with Claude Code" footers
- Any automated attribution or AI signatures

All commits should appear as authored solely by the repository owner.

## Support

- **Documentation**: See `/docs/` directory
- **Issues**: GitHub Issues for bug reports
- **Discussions**: GitHub Discussions for questions
- **Commercial**: Contact SenHub for enterprise support

---

**For complete development documentation, start with [Developer Guide](./docs/developer-guide/README.md).**

Last updated: 2025-12-09
