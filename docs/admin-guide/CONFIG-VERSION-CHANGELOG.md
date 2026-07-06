# Configuration Version Changelog

This document tracks changes to the SenHub Agent configuration file format.

## Format

Each configuration version is documented with:
- **Version**: Config format version number
- **Agent Versions**: Which agent versions support this config format
- **Date**: When this version was introduced
- **Breaking Changes**: Changes that require migration
- **New Features**: New configuration options
- **Migration**: How to migrate from previous version

---

## Version 3 (Current)

**Agent Versions**: 0.5.0+
**Status**: ✅ Current

### New Features
- **Secret references**: Inline plaintext secrets found in the config
  are sealed into the OS-native secret store and replaced by
  `${secret:<instance>.<field>}` references.

### Behaviour
- The version bump to `3` happens **only** when a config that actually
  contained an inline secret is sealed. A secret-free version 2 config
  stays at version 2 and loads unchanged — there is no unconditional
  version 2 → version 3 rewrite.
- The `config_version` field is stamped by the agent after sealing; it
  is not something you set by hand.
- An older agent (maximum supported version 2) refuses a version 3
  config rather than passing an unresolved `${secret:}` literal to a
  probe. Update the agent before deploying a sealed config.

### Migration from version 2
No manual migration is required. Sealing (which produces the
`${secret:...}` references) is what stamps `config_version: 3`; a
config without inline secrets is unaffected and remains at version 2.

---

## Version 2

**Agent Versions**: 0.1.65+
**Date**: 2025-10-13
**Status**: Supported

### Breaking Changes
- Probes now require **both** `name` and `type` fields
  - `name`: Display name (free-form, used in UI)
  - `type`: Technical probe identifier (used for constructor lookup)

### New Features
- **`config_version` field**: Explicit version tracking in YAML
- **`type` field for probes**: Separation of display name and technical ID
- **Multiple probe instances**: Same type with different names (e.g., "Prod Citrix", "Backup Citrix")
- **Automatic migration**: Version 1 configs are automatically migrated to version 2

### Configuration Example

```yaml
config_version: 2

agent:
  key: "your-agent-key"
  mode: offline

probes:
  # Multiple instances of the same probe type
  - name: Production Citrix      # Display name (free choice)
    type: citrix                 # Probe type (technical ID)
    params:
      base_url: "https://director-prod.company.com"
      interval: 120

  - name: Backup Citrix          # Different name, same type
    type: citrix                 # Same probe type
    params:
      base_url: "https://director-backup.company.com"
      interval: 120
```

### Migration from version 1

**Automatic Migration:**
- Agent automatically detects version 1 format (no `type` field)
- Creates backup: `agent-config.yaml.backup.YYYYMMDD-HHMMSS`
- Adds `config_version: 2`
- Adds `type` field to all probes (copies from `name`)
- Saves migrated config with header documentation

**Manual Migration:**
If you need to manually migrate:
1. Add `config_version: 2` at the top of your YAML
2. For each probe, add `type` field with the probe type
3. Optionally update `name` to a descriptive display name

**Before (version 1):**
```yaml
probes:
  - name: citrix
    params:
      base_url: "https://director.company.com"
```

**After (version 2):**
```yaml
config_version: 2

probes:
  - name: Production Citrix    # Display name (customizable)
    type: citrix               # Probe type (must match registry)
    params:
      base_url: "https://director.company.com"
```

---

## Version 1 (Legacy)

**Agent Versions**: 0.1.0 - 0.1.63
**Date**: 2024-09-01
**Status**: ⚠️ Legacy (automatic migration available)

### Characteristics
- No explicit `config_version` field in YAML
- Probes use single `name` field for both display and type
- No support for multiple instances of same probe type

### Configuration Example

```yaml
agent:
  key: "your-agent-key"
  mode: offline

probes:
  - name: cpu           # Used for both display AND type lookup
    params:
      interval: 30

  - name: citrix        # Can't have multiple Citrix probes
    params:
      base_url: "https://director.company.com"
```

### Limitations
- ❌ Cannot run multiple instances of the same probe type
- ❌ Probe names must match technical identifiers (cpu, citrix, etc.)
- ❌ No display name customization
- ❌ Harder to identify probes in UI (generic names)

---

## Compatibility Matrix

| Config Version | Agent Version | Status | Auto-Migration |
|---------------|---------------|--------|----------------|
| 1 | 0.1.0 - 0.1.63 | Legacy | ✅ Yes (1→2) |
| 2 | 0.1.65+ | Supported | N/A |
| 3 | 0.5.0+ | Current | On secret seal (2→3) |

## Version Detection

The agent automatically detects the configuration version:

1. **Explicit version**: If `config_version` field exists, use that value
2. **Implicit version 1**: If no `config_version` field, assume version 1 (legacy)
3. **Validation**: Agent validates compatibility with current version
4. **Migration**: If needed, automatic migration is triggered

## Future Versions

Future config versions will be documented here with:
- Clear breaking changes
- Migration guides
- Compatibility information
- New features and enhancements

---

## FAQ

### How do I know which config version I have?

Check the top of your `agent-config.yaml`:
- **Has `config_version: 3`**: You have version 3 (current — reached
  after inline secrets were sealed into the OS secret store)
- **Has `config_version: 2`**: You have version 2 (still supported)
- **No `config_version` field**: You have version 1 (legacy)

### Will my old config still work?

✅ Yes! Version 1 configs are automatically migrated to version 2 on agent startup.

### Can I manually edit config_version?

⚠️ **Not recommended**. The agent manages this field automatically. Manually editing it may cause compatibility issues.

### What happens if my config is too new?

If your config version is newer than what the agent supports, the agent will:
1. Log an error with compatibility details
2. Refuse to start
3. Prompt you to update the agent to a newer version

### Where are backups stored?

Backups are created in the same directory as your config file with format:
```
agent-config.yaml.backup.YYYYMMDD-HHMMSS
```

Example: `agent-config.yaml.backup.20251013-143022`

### How do I revert a migration?

If you need to revert:
1. Stop the agent
2. Restore the backup file:
   ```bash
   cp agent-config.yaml.backup.20251013-143022 agent-config.yaml
   ```
3. Downgrade agent to compatible version (or accept re-migration)

---

## Technical Details

### Version Validation

The agent performs these checks on startup:

```go
// Check compatibility
if configVersion < MinimumConfigVersion {
    // Error: Too old
}

if configVersion > CurrentConfigVersion {
    // Error: Too new, update agent
}

if configVersion < CurrentConfigVersion {
    // Trigger automatic migration
}
```

### Migration Process

1. **Detection**: Agent detects old format (missing `type` field or `config_version`)
2. **Backup**: Creates timestamped backup with agent version metadata
3. **Transform**: Adds `config_version` field and `type` to all probes
4. **Header**: Adds migration documentation header to file
5. **Validation**: Validates migrated config
6. **Save**: Writes migrated config to disk
7. **Continue**: Agent continues startup with new config

---

**Current Version**: 3
**Maintainer**: SenHub Agent Team
