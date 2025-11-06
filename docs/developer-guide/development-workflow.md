# Development Workflow

This document describes the development workflow for SenHub Agent, including version management, branching strategy, and code review guidelines.

## Version Management

### Version Scheme
- **Production version**: Always tagged without `-beta` suffix (e.g., `0.1.64`)
- **Development version**: Next version with `-beta` suffix (e.g., `0.1.66-beta`)
- **Current prod**: `0.1.64`
- **Next dev**: `0.1.66-beta`

### Version Tag Format
- **IMPORTANT**: All version tags must follow the format `X.Y.Z-beta` (WITHOUT the "v" prefix)
- Example: `0.1.66-beta` (correct) vs `v0.1.66-beta` (incorrect)
- Beta releases are automatically generated from dev branch pushes
- Workflow uses `git describe --tags --abbrev=0` to find latest tag

## Branch Strategy

### 1. Feature Branches
Create a new branch for each feature or fix:
```bash
git checkout -b feature/my-feature-name
```

Feature branches allow:
- Independent development and testing
- Clean separation of work
- Easy rollback if needed
- Multiple features in parallel

### 2. Local Development
ALL work stays local until user approval:

```bash
# Build and test locally
make build-darwin
make build-windows
./dist/senhub-agent_darwin_amd64 run --verbose

# Commit locally as needed
git add .
git commit -m "fix: authentication key handling"

# User tests manually with local binaries
```

**Critical Rules:**
- Commit locally as needed
- Build and test locally: `make build-darwin`, `make build-windows`
- User tests manually with local binaries
- DO NOT push to remote until approved

### 3. Merge to Dev
ONLY when feature is sufficiently advanced and tested:

```bash
git checkout dev
git merge feature/my-feature-name
# Stay local - DO NOT PUSH yet

# Final build and test
make build-darwin build-windows
# Final user testing...
```

**Important**: Merge stays local until final approval.

### 4. Push to Remote
ONLY after user's personal testing and explicit approval:

```bash
git push origin dev
# Beta release will be triggered automatically
```

## Complete Example Workflow

```bash
# 1. Start new feature
git checkout -b feature/fix-auth-key
make build-darwin
./dist/senhub-agent_darwin_amd64 run --verbose

# 2. Make changes, commit locally
git add .
git commit -m "fix: authentication key handling"
make build-windows
# User tests on Windows...

# 3. Feature complete and tested - merge to dev (LOCAL)
git checkout dev
git merge feature/fix-auth-key
make build-darwin build-windows
# Final user testing...

# 4. User approves - push to remote
git push origin dev
# Beta release will be triggered automatically
```

## Critical Rules Summary

- ⛔ **NO automatic pushes** to remote repositories
- ⛔ **NO beta releases** until user approval
- ⛔ **NO commits directly to dev** - always use feature branches
- ✅ **YES to local builds** for testing (darwin, windows, linux)
- ✅ **YES to local commits** on feature branches
- ✅ **YES to feature branches** that can be built and tested independently

## Code Review Guidelines (MANDATORY for code-reviewer agent)

### Critical Verification Checklist

When reviewing code changes, the code-reviewer agent MUST systematically verify:

#### 1. Test Coverage and Updates (CRITICAL)
- ✅ **Are tests updated?** For EVERY code change, verify corresponding tests exist and are current
- ✅ **New functionality?** → New tests MUST be added
- ✅ **Modified behavior?** → Existing tests MUST be updated to reflect changes
- ✅ **API changes?** → Integration tests MUST be updated
- ✅ **Bug fixes?** → Regression tests MUST be added
- ⚠️ **Red flag:** Code changes without corresponding test updates

**Examples of test mismatches to catch:**
- Function signature changed but tests still use old signature
- New struct fields added but tests don't verify them
- New error cases added but tests don't cover them
- Method behavior changed (e.g., GetName() now inherited from BaseProbe) but tests still expect old hardcoded behavior
- New configuration options added but no validation tests

#### 2. Test Execution Status
- ✅ Verify tests actually pass (check CI/CD status or ask to run tests)
- ✅ No skipped tests without justification
- ✅ No commented-out test cases without explanation
- ✅ Test assertions are meaningful (not just checking `err == nil`)

#### 3. Code Quality Checks
- ✅ Proper error handling with context
- ✅ No hardcoded values (use constants/config)
- ✅ Thread safety for concurrent code
- ✅ Resource cleanup (defer Close(), context cancellation)
- ✅ Logging at appropriate levels

#### 4. Architecture Compliance
- ✅ Follows established patterns (BaseProbe embedding, delegation, etc.)
- ✅ Respects separation of concerns
- ✅ No breaking changes to public APIs without deprecation
- ✅ Documentation updated for behavior changes

### Review Process

1. **Analyze code changes** → Identify modified/new functionality
2. **Locate test files** → Find corresponding `*_test.go` files
3. **Compare implementations** → Verify tests reflect current code behavior
4. **Check test results** → Confirm all tests pass in CI/CD
5. **Flag mismatches** → Report any tests that need updates
6. **Provide specific guidance** → Suggest exact test updates needed

### Review Severity Levels

- 🔴 **BLOCKER**: Tests missing for new code, tests failing, critical bugs
- 🟠 **MAJOR**: Tests outdated, incomplete coverage, architectural violations
- 🟡 **MINOR**: Style issues, missing comments, optimization opportunities
- 🟢 **SUGGESTION**: Improvements, refactoring ideas, best practices

### Example Review Output Format

```
## Test Coverage Analysis
✅ Unit tests present: Yes
⚠️ Tests outdated: cpu/cpuProbe_test.go line 70 expects hardcoded "cpu" but GetName() now inherited
❌ Missing tests: No tests for new SetProbeType() method

## Required Actions
1. Update cpu/cpuProbe_test.go:70 to call SetName("cpu") before testing GetName()
2. Add test case for SetProbeType() and GetProbeType() methods
3. Verify tests pass: `go test ./internal/agent/probes/cpu -v`
```

## Git Commit Guidelines

### Commit Messages
Follow conventional commits format:
```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code refactoring
- `test`: Test updates
- `docs`: Documentation updates
- `chore`: Maintenance tasks

**Examples:**
```bash
git commit -m "feat(probe): add Redfish probe support"
git commit -m "fix(citrix): exclude reconnections from logon duration"
git commit -m "refactor(http): modularize HTTP strategy with managers"
```

### Commit Signatures
- **DO NOT** add "Co-Authored-By: Claude" signatures
- **DO NOT** add "Generated with Claude Code" footers
- **DO NOT** add any automated attribution or AI signatures
- All commits should appear as authored solely by the repository owner
- Focus commit message on what was changed and why

## Release Process

### Beta Releases
1. Features merged to `dev` branch (locally)
2. User approves and pushes to remote
3. GitHub Actions automatically:
   - Runs tests
   - Builds binaries for all platforms
   - Creates beta release with tag `X.Y.Z-beta`
   - Generates release notes

### Production Releases
1. Beta tested thoroughly in production environments
2. User approves promotion to production
3. Merge `dev` to `master`
4. Tag with production version (without `-beta`)
5. GitHub Actions creates production release

## Configuration Management

### Configuration Format (v2)
Probe configuration uses name/type system:
- `name`: Display name (free-form, used for UI identification)
- `type`: Probe type (technical identifier for constructor lookup)

Example:
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

### Automatic Configuration Migration
Zero-downtime migration system automatically:
1. Detects old config format (missing `type` field)
2. Creates timestamped backup: `agent-config.yaml.backup.YYYYMMDD-HHMMSS`
3. Adds migration header with agent version and timestamp
4. Transforms config: adds `type` field (copies from `name`)
5. Saves migrated config
6. Agent continues startup with migrated config

Benefits:
- No breaking changes for existing deployments
- Automatic migration preserves user data
- Clear audit trail with backups and headers
- Clean code without legacy fallback logic

## Next Steps

- Review [Build System](./build-system.md) for compilation and testing
- Study [Design Patterns](./design-patterns.md) before writing code
- Check [Current Development](./current-development.md) for active work

---

Last updated: 2025-11-06
