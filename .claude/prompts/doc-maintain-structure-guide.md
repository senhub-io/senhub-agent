# Documentation Structure Guide for doc-maintain Agent

**Version**: 1.0
**Date**: 2025-11-06
**Purpose**: Ensure consistent documentation organization across the SenHub Agent project

---

## 📁 Standard Directory Structure

The SenHub Agent documentation follows this structure:

```
senhub-agent/
├── README.md                    # Project overview (short, welcoming)
├── CLAUDE.md                    # Lightweight dev guide pointer (max 10KB)
├── LICENSE
├── Makefile
│
├── /docs/
│   ├── _Sidebar.md             # GitHub Wiki navigation (REQUIRED)
│   ├── Home.md                 # GitHub Wiki landing page (REQUIRED)
│   ├── README.md               # Documentation hub (main index)
│   ├── DOCUMENTATION-INDEX.md  # Comprehensive documentation index
│   │
│   ├── /releases/              # All release notes (organized)
│   │   ├── README.md           # Release history index
│   │   ├── CHANGELOG.md        # Consolidated changelog
│   │   ├── YYYY-MM-DD-vX.Y.Z-beta.md  # Beta releases
│   │   └── vX.Y.Z.md           # Production releases
│   │
│   ├── /developer-guide/       # Development documentation (modular)
│   │   ├── README.md           # Developer guide home
│   │   ├── architecture.md     # System architecture
│   │   ├── development-workflow.md  # Git workflow, branching
│   │   ├── build-system.md     # Makefile, compilation
│   │   ├── design-patterns.md  # Code patterns and best practices
│   │   ├── current-development.md  # Active work and roadmap
│   │   └── /engineering/       # Engineering design docs
│   │       ├── TIME_SERIES_KEY_DESIGN.md
│   │       └── DISCRIMINANT-TAGS-REGISTRY.md
│   │
│   ├── /user-guide/            # End-user documentation
│   │   ├── README.md
│   │   ├── /installation/
│   │   ├── /configuration/
│   │   └── /troubleshooting/
│   │
│   ├── /admin-guide/           # System administration docs
│   │   ├── README.md
│   │   ├── HTTP-STRATEGY.md
│   │   ├── HTTPS-CONFIGURATION.md
│   │   └── ...
│   │
│   ├── /probes/                # Probe-specific documentation
│   │   ├── /system/
│   │   │   ├── /cpu/
│   │   │   │   ├── README.md
│   │   │   │   └── METRICS.md
│   │   │   └── ...
│   │   ├── /network/
│   │   ├── /citrix/
│   │   ├── /redfish/
│   │   └── ...
│   │
│   ├── /troubleshooting/       # Troubleshooting guides
│   │   ├── README.md
│   │   └── ...
│   │
│   ├── /presentations/         # Sales/demo materials
│   │   └── redfish-client-demo.md
│   │
│   ├── /archive/               # Deprecated/historical docs
│   │   └── README.md
│   │
│   └── /.internal/             # Internal documentation (hidden)
│       ├── DOCUMENTATION-STRUCTURE.md
│       └── /claude-config/     # Claude Code personal configs
│           └── ...
│
├── /internal/                  # Go source code
│   └── (source directories may have occasional READMEs)
│
└── /.github/
    └── /workflows/
```

---

## 📝 File Placement Rules

### Where to Place New Documentation

| Document Type | Location | Example |
|---------------|----------|---------|
| Release notes | `/docs/releases/` | `0.1.66-beta.md` |
| Developer guides | `/docs/developer-guide/` | `architecture.md` |
| User guides | `/docs/user-guide/` | `installation.md` |
| Admin guides | `/docs/admin-guide/` | `https-config.md` |
| Probe documentation | `/docs/probes/{category}/{probe}/` | `redfish/README.md` |
| Troubleshooting | `/docs/troubleshooting/` | `offline-issues.md` |
| Presentations | `/docs/presentations/` | `client-demo.md` |
| Internal configs | `/docs/.internal/` | `claude-config/` |

### What Should NOT Be at Root

❌ **Do NOT place at root:**
- Release notes (goes to `/docs/releases/`)
- Detailed development guides (goes to `/docs/developer-guide/`)
- Presentations (goes to `/docs/presentations/`)
- Internal configurations (goes to `/docs/.internal/`)

✅ **Only at root:**
- `README.md` - Project overview (short, welcoming)
- `CLAUDE.md` - Lightweight pointer to developer guide (max 10KB)
- `LICENSE` - Project license
- `Makefile` - Build system

---

## 📏 Naming Conventions

### File Naming

| Type | Convention | Examples |
|------|------------|----------|
| README files | `README.md` (uppercase) | `README.md` |
| Release notes | `vX.Y.Z-beta.md` or `vX.Y.Z.md` | `0.1.66-beta.md` |
| Guide documents | `kebab-case.md` | `development-workflow.md` |
| Probe docs | `README.md` + `METRICS.md` | `cpu/README.md` |
| Presentations | `descriptive-name.md` | `redfish-client-demo.md` |

### Directory Naming

| Type | Convention | Examples |
|------|------------|----------|
| Main categories | `kebab-case/` | `developer-guide/`, `user-guide/` |
| Probe categories | `lowercase/` | `system/`, `network/`, `citrix/` |
| Probe names | `lowercase/` | `cpu/`, `memory/`, `redfish/` |
| Internal dirs | `.lowercase/` | `.internal/` |

---

## 🔗 GitHub Wiki Integration

### Required Files for Wiki

1. **`docs/_Sidebar.md`** (REQUIRED)
   - Navigation menu for GitHub Wiki
   - Must be updated when adding new sections
   - Keep concise (max 20 links)

2. **`docs/Home.md`** (REQUIRED)
   - Landing page for GitHub Wiki
   - Welcoming introduction
   - Quick links to main sections

### Wiki Sync Strategy

- **Manual Sync**: Currently done manually
- **Excluded from Wiki**: `/docs/.internal/` directory
- **Future**: Automated sync via GitHub Actions

---

## 🎨 Document Structure

### Standard Document Template

```markdown
# Document Title

**Status**: [Draft/Review/Published]
**Version**: X.Y
**Last Updated**: YYYY-MM-DD

## Overview

[Brief description of what this document covers]

## Table of Contents

- [Section 1](#section-1)
- [Section 2](#section-2)

## Section 1

[Content]

## Section 2

[Content]

---

**Related Documentation**:
- [Link to related doc 1](./related-doc-1.md)
- [Link to related doc 2](./related-doc-2.md)
```

### Release Notes Template

Location: `/docs/releases/vX.Y.Z-beta.md`

```markdown
# Release Notes - SenHub Agent vX.Y.Z-beta

**Release Date**: YYYY-MM-DD
**Branch**: dev
**Target**: [Production/Testing]

## Overview

[Brief summary of release]

## Critical Fixes

[Description of critical bugs fixed]

## Improvements

[Description of improvements]

## Breaking Changes

[Any breaking changes]

## Migration Path

[How users should upgrade]

## Known Issues

[Known issues in this release]

---

**Status**: ✅ Ready for Production Testing
```

---

## 🔄 Maintenance Procedures

### When Adding New Documentation

1. **Determine Audience**:
   - User-facing → `/docs/user-guide/`
   - Admin-facing → `/docs/admin-guide/`
   - Developer-facing → `/docs/developer-guide/`
   - Internal → `/docs/.internal/`

2. **Check Existing Structure**:
   - Is there an existing category?
   - Follow existing patterns
   - Update parent README with link

3. **Update Navigation**:
   - Add to `docs/README.md` (main index)
   - Add to `docs/_Sidebar.md` (wiki navigation)
   - Add to parent section README

4. **Cross-Reference**:
   - Link to related documentation
   - Add "Related Documentation" section at bottom

### When Moving Documentation

1. **Update All Links**:
   - Use `grep -r "old-path" docs/` to find references
   - Update relative links
   - Test all links work

2. **Leave Redirects** (if needed):
   - For important moved docs, consider a redirect stub
   - Example: "This document moved to [new location](./new-path.md)"

3. **Update Git History**:
   - Use `git mv` to preserve history
   - Commit with clear message explaining move

### When Deprecating Documentation

1. **Move to Archive**:
   - Move to `/docs/archive/`
   - Add "DEPRECATED" notice at top
   - Link to replacement documentation

2. **Update References**:
   - Remove from main indexes
   - Update cross-references
   - Keep for historical reference

---

## 🎯 Quality Standards

### Documentation Quality Checklist

- [ ] Clear title and purpose
- [ ] Table of contents (if >3 sections)
- [ ] Code examples formatted correctly
- [ ] Links tested and working
- [ ] Appropriate audience level
- [ ] No sensitive information exposed
- [ ] Follows naming conventions
- [ ] Added to navigation menus
- [ ] Cross-referenced appropriately

### Content Guidelines

**Do**:
- ✅ Use clear, concise language
- ✅ Provide practical examples
- ✅ Include troubleshooting tips
- ✅ Link to related documentation
- ✅ Keep content up-to-date

**Don't**:
- ❌ Include sensitive credentials
- ❌ Use absolute URLs for internal links
- ❌ Duplicate content across files
- ❌ Create orphaned documents
- ❌ Use unclear abbreviations

---

## 🤖 Agent Instructions

### For doc-maintain Agent

When maintaining documentation:

1. **Always Check Structure First**:
   ```bash
   # Verify structure matches guide
   ls -R docs/
   ```

2. **Follow Placement Rules**:
   - Release notes → `/docs/releases/`
   - Development docs → `/docs/developer-guide/`
   - Never place docs at root (except README.md, CLAUDE.md)

3. **Update Navigation Files**:
   - `docs/README.md` (main index)
   - `docs/_Sidebar.md` (wiki nav)
   - Parent section README

4. **Verify Links**:
   ```bash
   # Find all markdown links
   grep -r "\[.*\](.*)" docs/
   # Test each link exists
   ```

5. **Maintain CLAUDE.md**:
   - Keep lightweight (max 10KB)
   - Point to `/docs/developer-guide/`
   - Include only critical quick reference

6. **Before Committing**:
   - Run link checker
   - Verify file placement
   - Update all indexes
   - Test navigation works

---

## 📚 Examples

### Example: Adding a New Probe Documentation

```bash
# 1. Create probe directory
mkdir -p docs/probes/monitoring/newprobe/

# 2. Create documentation files
touch docs/probes/monitoring/newprobe/README.md
touch docs/probes/monitoring/newprobe/METRICS.md

# 3. Update parent README
echo "- [New Probe](./newprobe/README.md)" >> docs/probes/monitoring/README.md

# 4. Update main index
# Edit docs/README.md to add link

# 5. Update wiki sidebar
# Edit docs/_Sidebar.md to add link (if major probe)
```

### Example: Adding a Release Note

```bash
# 1. Create release note
vim docs/releases/0.1.67-beta.md

# 2. Update releases index
# Add link to docs/releases/README.md

# 3. Update CHANGELOG.md
# Add entry to docs/releases/CHANGELOG.md

# 4. Commit
git add docs/releases/
git commit -m "docs: add release notes for v0.1.67-beta"
```

### Example: Moving a Document

```bash
# 1. Move file (preserves history)
git mv docs/old-location.md docs/new-location/document.md

# 2. Find all references
grep -r "old-location.md" docs/

# 3. Update all links
# Edit each file found in step 2

# 4. Update indexes
# Edit docs/README.md, docs/_Sidebar.md, etc.

# 5. Commit
git add -A
git commit -m "docs: move document to new location"
```

---

## 🔍 Verification Commands

### Check Structure Compliance

```bash
# Verify no docs at root (except README.md, CLAUDE.md)
ls -la *.md | grep -v "README.md\|CLAUDE.md\|LICENSE"

# Verify releases directory exists
test -d docs/releases && echo "✅ Releases dir exists" || echo "❌ Missing releases dir"

# Verify wiki files exist
test -f docs/_Sidebar.md && test -f docs/Home.md && echo "✅ Wiki files exist" || echo "❌ Missing wiki files"

# Check for orphaned docs (no links to them)
# (requires custom script)
```

### Find Broken Links

```bash
# Find all markdown links
grep -r "\[.*\](.*)" docs/ | grep -o "([^)]*)" | sed 's/[()]//g'

# Check if each file exists
# (requires custom validation script)
```

---

## 📈 Metrics to Track

### Documentation Health Indicators

- **Organization**: Are files in correct directories?
- **Navigation**: Can users find what they need?
- **Completeness**: Are all features documented?
- **Accuracy**: Is documentation up-to-date?
- **Link Health**: Are all links working?

---

## 🎓 Training Resources

### For New Contributors

1. Read this guide completely
2. Review existing documentation structure
3. Look at examples in each section
4. Ask questions if unclear

### For Automated Agents

1. Always consult this guide before making changes
2. Follow placement rules strictly
3. Update all navigation files
4. Verify links after changes
5. Report any structure violations

---

**Version History**:
- 1.0 (2025-11-06): Initial structure guide based on v0.1.66-beta reorganization

**Maintainer**: Development Team
**Review Schedule**: Quarterly (every 3 months)
