# Documentation Structure Guide

This document explains the documentation organization for SenHub Agent and provides guidelines for maintaining it.

## Directory Structure

```
docs/
├── Home.md                      # GitHub Wiki landing page
├── _Sidebar.md                  # GitHub Wiki navigation menu
├── README.md                    # Documentation index
│
├── user-guide/                  # End-user documentation
│   ├── GETTING-STARTED.md       # Online mode quick start
│   ├── QUICK-START-OFFLINE.md   # Offline mode quick start
│   ├── INSTALLATION.md          # Detailed installation
│   ├── OFFLINE-MODE.md          # Offline mode guide
│   └── CONFIGURATION.md         # Configuration overview
│
├── admin-guide/                 # Administration documentation
│   ├── HTTPS-CONFIGURATION.md   # TLS/SSL setup
│   ├── UNIVERSAL-CONFIGURATION.md  # Config validation API
│   └── MONITORING-INTEGRATION.md   # Integration guides
│
├── probes/                      # Probe-specific documentation
│   ├── redfish/                 # Redfish probe
│   ├── citrix/                  # Citrix probe
│   ├── system/                  # System probes (cpu, memory, etc.)
│   │   ├── cpu/
│   │   ├── memory/
│   │   ├── network/
│   │   └── logicaldisk/
│   ├── network/                 # Network probes (gateway, wifi)
│   ├── webapp/                  # WebApp monitoring
│   ├── events/                  # Event probes (syslog)
│   ├── winevents/               # Windows events
│   └── otel/                    # OpenTelemetry
│
├── developer-guide/             # Developer documentation
│   ├── README.md                # Developer guide home
│   ├── architecture.md          # System architecture
│   ├── development-workflow.md  # Git workflow, branching
│   ├── build-system.md          # Makefile, compilation
│   ├── design-patterns.md       # Code patterns
│   ├── current-development.md   # Active work, roadmap
│   └── engineering/             # Technical deep-dives
│       ├── TIME_SERIES_KEY_DESIGN.md
│       └── DISCRIMINANT-TAGS-REGISTRY.md
│
├── releases/                    # Release notes
│   ├── README.md                # Release index
│   └── X.Y.Z-beta.md            # Version-specific notes
│
├── presentations/               # Presentations and demos
│   └── redfish-client-demo.md
│
├── troubleshooting/             # Troubleshooting guides
│   ├── COMMON-ISSUES.md
│   └── FAQ.md
│
└── .internal/                   # Internal documentation
    ├── claude-config/           # Claude Code configuration
    └── DOCUMENTATION-STRUCTURE.md  # This file
```

## Documentation Types

### User-Facing Documentation
**Location**: `user-guide/`, `admin-guide/`, `probes/`
**Audience**: End users, system administrators
**Style**: Tutorial-based, task-oriented
**Sync**: Automatically synced to GitHub Wiki

**Guidelines**:
- Start with quick start guides
- Use step-by-step instructions
- Include examples and screenshots
- Avoid technical jargon
- Focus on what users need to accomplish

### Developer Documentation
**Location**: `developer-guide/`
**Audience**: Contributors, developers
**Style**: Technical, reference-oriented
**Sync**: Not synced to GitHub Wiki (kept in repository)

**Guidelines**:
- Explain architecture and design decisions
- Document patterns and best practices
- Include code examples
- Link to relevant source files
- Keep current with codebase changes

### Internal Documentation
**Location**: `.internal/`
**Audience**: Maintainers, automation tools
**Style**: Procedural, maintenance-oriented
**Sync**: Never synced (internal use only)

**Guidelines**:
- Document maintenance procedures
- Include automation configurations
- Keep private information here
- Don't expose in wiki

## Naming Conventions

### Files
- **ALL-CAPS-WITH-HYPHENS.md**: Major documents (README.md, GETTING-STARTED.md)
- **lowercase-with-hyphens.md**: Regular documents (architecture.md, build-system.md)
- **version.md**: Release notes (0.1.66-beta.md)

### Directories
- **lowercase-with-hyphens**: All directories (user-guide/, admin-guide/)
- **No spaces**: Never use spaces in directory names

### Headers
- **Title Case**: Document titles (# Getting Started)
- **Sentence case**: Section headers (## Installing on Windows)

## Adding New Documentation

### New User Guide
1. Create file in `user-guide/` with appropriate name
2. Add to `_Sidebar.md` under "User Guide" section
3. Link from relevant getting started guides
4. Update `README.md` if major addition

### New Probe Documentation
1. Create directory under `probes/<category>/`
2. Add README.md with probe overview
3. Include configuration examples
4. Add metrics reference
5. Link from `_Sidebar.md` under "Probes"

### New Developer Guide Section
1. Create file in `developer-guide/`
2. Link from `developer-guide/README.md`
3. Add to `_Sidebar.md` under "Developer Guide"
4. Cross-link with related documents

### New Release Notes
1. Create `docs/releases/X.Y.Z-beta.md`
2. Follow release notes template
3. Update `releases/README.md` with link
4. Update "Latest Release" link in `_Sidebar.md`

## Link Management

### Internal Links
Use relative paths from current document location:
```markdown
[Architecture](./architecture.md)                    # Same directory
[User Guide](../user-guide/GETTING-STARTED.md)      # Parent directory
[Redfish Probe](../probes/redfish/README.md)        # Sibling directory
```

### External Links
Use full URLs for external resources:
```markdown
[Go Documentation](https://golang.org/doc/)
[GitHub Repository](https://github.com/sen-hub/senhub-agent)
```

### Cross-References
Always link to the canonical location:
```markdown
See [Development Workflow](../developer-guide/development-workflow.md) for details.
```

## Maintaining Documentation

### When to Update

**Code Changes**:
- New features → Update relevant user/admin guides
- API changes → Update developer documentation
- Bug fixes → Update troubleshooting if applicable

**Configuration Changes**:
- New options → Update configuration documentation
- Format changes → Update migration guides
- Deprecated features → Add deprecation notices

**Release**:
- Create release notes in `releases/`
- Update version references in documents
- Update "Latest Release" links

### Update Checklist

When modifying documentation:
- [ ] Update all affected documents
- [ ] Check and update internal links
- [ ] Update `_Sidebar.md` if structure changed
- [ ] Update `README.md` if major addition
- [ ] Verify no broken links
- [ ] Check formatting renders correctly

### Link Validation

Periodically check for broken links:
```bash
# Find all markdown links
grep -r "\[.*\](.*)" docs/ --include="*.md"

# Check for broken internal links
# (manually verify file exists)
```

## GitHub Wiki Sync

### What Gets Synced
- `user-guide/` - All end-user documentation
- `admin-guide/` - All admin documentation
- `probes/` - All probe documentation
- `troubleshooting/` - All troubleshooting guides
- `releases/` - All release notes
- `Home.md` and `_Sidebar.md` - Wiki structure

### What Doesn't Get Synced
- `developer-guide/` - Kept in repository only
- `.internal/` - Never synced
- `archive/` - Historical content

### Sync Configuration
Configure GitHub Actions workflow to sync on push:
```yaml
# .github/workflows/wiki-sync.yml
```

## Documentation Standards

### Writing Style
- **Clear and concise**: Avoid unnecessary complexity
- **Active voice**: "Click the button" not "The button should be clicked"
- **Present tense**: "The agent starts" not "The agent will start"
- **Second person**: "You can configure" not "One can configure"

### Code Examples
Always include:
- Language identifier for syntax highlighting
- Comments explaining non-obvious parts
- Complete, runnable examples when possible

```bash
# Good: Clear, commented, complete
# Install SenHub Agent in offline mode
./senhub-agent install --offline

# Start the agent service
./senhub-agent start
```

### Images
- Store in `docs/images/<category>/`
- Use descriptive filenames
- Reference with relative paths
- Include alt text for accessibility

```markdown
![Agent Dashboard](../images/user-guide/dashboard.png)
```

## Templates

### Release Notes Template
```markdown
# Release X.Y.Z-beta

Released: YYYY-MM-DD

## Overview
Brief description of the release.

## New Features
- Feature 1
- Feature 2

## Improvements
- Improvement 1
- Improvement 2

## Bug Fixes
- Fix 1
- Fix 2

## Breaking Changes
- Breaking change 1 (if any)

## Known Issues
- Issue 1 (if any)

## Upgrade Notes
Instructions for upgrading (if needed).
```

### Probe Documentation Template
```markdown
# [Probe Name] Probe

## Overview
Brief description of what the probe monitors.

## Configuration

### Basic Configuration
```yaml
- name: probe-name
  type: probe-type
  params:
    param1: value1
    param2: value2
```

### Advanced Configuration
Advanced options and examples.

## Metrics
List of metrics collected.

## Troubleshooting
Common issues and solutions.
```

## Maintenance Procedures

### Monthly Review
- Check for outdated information
- Update version references
- Fix broken links
- Review feedback and issues

### Quarterly Reorganization
- Evaluate documentation structure
- Move archived content
- Update navigation
- Consolidate duplicated information

### Annual Overhaul
- Major structure review
- Complete link validation
- Style consistency check
- Update all screenshots

## Tools and Automation

### Markdown Linting
```bash
# Use markdownlint for consistency
markdownlint docs/**/*.md
```

### Link Checking
```bash
# Check for broken links
find docs -name "*.md" -exec grep -l "](.*)" {} \;
```

### Documentation Generation
- Use scripts to generate metric references
- Automate API documentation from code
- Generate probe lists from registry

## Questions?

For documentation-related questions:
- Check this guide first
- Review existing documentation for patterns
- Ask maintainers for guidance

---

Last updated: 2025-11-06
