# Developer Guide

Welcome to the SenHub Agent Developer Guide. This documentation provides comprehensive guidance for contributing to and extending the SenHub Agent.

## Quick Navigation

### Core Documentation
- **[Architecture](./architecture.md)** - System design, patterns, and component architecture
- **[Development Workflow](./development-workflow.md)** - Git workflow, branching strategy, and release process
- **[Build System](./build-system.md)** - Makefile commands, compilation, and packaging
- **[Design Patterns](./design-patterns.md)** - Code patterns, best practices, and compliance checklist
- **[Current Development](./current-development.md)** - Active work, roadmap, and feature status

### Specialized Topics
- **[Engineering Documentation](./engineering/)** - Technical deep-dives and implementation details
  - [Time Series Key Design](./engineering/TIME_SERIES_KEY_DESIGN.md)
  - [Discriminant Tags Registry](./engineering/DISCRIMINANT-TAGS-REGISTRY.md)

### Code Quality
- **[Code Review Guidelines](./development-workflow.md#code-review-guidelines)** - Mandatory checklist for reviewers
- **[Testing Best Practices](./build-system.md#testing-best-practices)** - Test execution and coverage
- **[Code Style Guidelines](./architecture.md#code-style-guidelines)** - Formatting and naming conventions

## Getting Started

### For New Contributors
1. Read the [Development Workflow](./development-workflow.md) to understand our Git process
2. Review [Architecture](./architecture.md) to understand the system structure
3. Check [Current Development](./current-development.md) to see what's being worked on
4. Study [Design Patterns](./design-patterns.md) before writing code

### For Code Reviewers
1. Review the [Code Review Guidelines](./development-workflow.md#code-review-guidelines)
2. Understand the [Pattern Compliance Checklist](./design-patterns.md#pattern-compliance-checklist)
3. Verify [Testing Requirements](./build-system.md#testing-best-practices)

## Critical Rules

- **NO automatic pushes** to remote repositories
- **NO beta releases** until user approval
- **NO commits directly to dev** - always use feature branches
- **ALWAYS use `make test`** instead of running `go test` directly
- **Feature branches first** - merge to dev only when sufficiently tested

## Development Environment

- **Work Directory**: `/Users/matthieu/Documents/GitHub/senhub-agent/`
- **Platform**: Cross-platform (darwin, windows, linux)
- **Language**: Go with gofmt enforcement
- **Testing**: Table-driven tests with comprehensive coverage

## Documentation Structure

This developer guide is part of a larger documentation system:
- `/docs/user-guide/` - End-user documentation
- `/docs/admin-guide/` - Administration and configuration
- `/docs/probes/` - Probe-specific documentation
- `/docs/developer-guide/` - This guide
- `/docs/.internal/` - Internal documentation and tooling

## Contributing

When adding new features or fixing bugs:
1. Create a feature branch: `git checkout -b feature/my-feature-name`
2. Build and test locally: `make build && make test`
3. Follow our [Design Patterns](./design-patterns.md)
4. Ensure tests pass and are updated
5. Wait for user approval before pushing to remote

## Support

For questions about development:
- Check this guide first
- Review existing code for patterns
- Consult the [Engineering Documentation](./engineering/)
- Ask the maintainers

---

Last updated: 2025-12-09
