# Release Notes

This directory contains release notes for all versions of SenHub Agent.

## Latest Releases

### Beta Releases (Development Branch)
- **[0.1.66-beta](./0.1.66-beta.md)** - Latest beta (2025-11-06)

## Release Channels

### Beta Channel
- **Branch**: `dev`
- **Purpose**: Active development, testing new features
- **Tag Format**: `X.Y.Z-beta` (e.g., `0.1.66-beta`)
- **Frequency**: On-demand when features are ready
- **Stability**: Tested but may contain bugs
- **Audience**: Early adopters, testers, development environments

### Production Channel
- **Branch**: `master`
- **Purpose**: Stable releases for production use
- **Tag Format**: `X.Y.Z` (e.g., `0.1.64`)
- **Frequency**: After thorough beta testing
- **Stability**: Production-ready
- **Audience**: Production environments

## Version Scheme

SenHub Agent uses semantic versioning:
- **Major version** (X): Breaking changes, major new features
- **Minor version** (Y): New features, non-breaking changes
- **Patch version** (Z): Bug fixes, minor improvements
- **Beta suffix** (-beta): Development releases

Examples:
- `0.1.64` - Production release
- `0.1.66-beta` - Beta release (development)

## Accessing Releases

### GitHub Releases
All releases are available on GitHub:
- Beta: https://github.com/sen-hub/senhub-agent/releases?q=beta
- Production: https://github.com/sen-hub/senhub-agent/releases

### Download Binaries
Each release includes binaries for:
- macOS (darwin_amd64)
- Linux (linux_amd64)
- Windows (windows_amd64.exe)

### Installation
```bash
# Download latest beta
wget https://github.com/sen-hub/senhub-agent/releases/download/0.1.66-beta/senhub-agent_darwin_amd64

# Make executable
chmod +x senhub-agent_darwin_amd64

# Install
./senhub-agent_darwin_amd64 install --authentication-key YOUR_KEY
```

## Release Notes Format

Each release note includes:
- **Overview**: Summary of changes
- **New Features**: New functionality added
- **Improvements**: Enhancements to existing features
- **Bug Fixes**: Issues resolved
- **Breaking Changes**: Changes requiring action
- **Known Issues**: Current limitations
- **Upgrade Notes**: Migration instructions if needed

## Release History

### v0.1.x Series (Current)
- [0.1.66-beta](./0.1.66-beta.md) - Configuration v2 migration, shared templates
- 0.1.64 - Latest production release
- Previous releases available in GitHub

## Upgrade Guidelines

### Beta to Beta
Generally safe to upgrade directly:
```bash
# Stop agent
./agent stop

# Replace binary
cp senhub-agent_new senhub-agent

# Start agent
./agent start
```

### Production to Production
Follow release notes for specific upgrade instructions.

### Beta to Production
Not recommended - always go through proper testing cycle.

## Contributing

When creating a new release:
1. Update version in code
2. Create release notes in this directory
3. Tag the commit with appropriate version
4. GitHub Actions will create the release automatically

See [Development Workflow](../developer-guide/development-workflow.md) for details.

## Support

For release-specific issues:
- Check the release notes for known issues
- Review the troubleshooting guide
- Contact support with version number

---

Last updated: 2025-11-06
