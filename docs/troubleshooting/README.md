# Troubleshooting Guide

This section contains troubleshooting guides and solutions for common issues when using SenHub Agent.

## 📋 Contents

### Mode-Specific Issues
- **[Offline Mode Issues](./TROUBLESHOOTING-OFFLINE.md)** - Common problems and solutions for offline mode

## 🎯 Who This Is For

- **Users** experiencing issues with SenHub Agent
- **System Administrators** diagnosing deployment problems
- **Support Teams** helping users resolve configuration issues

## 🚨 Common Issue Categories

### Configuration Issues
- Invalid YAML syntax
- Missing required parameters
- Incorrect file paths or permissions
- Network configuration problems

### Runtime Issues
- Agent fails to start
- Probes not collecting data
- API endpoints not responding
- Certificate/TLS problems

### Performance Issues
- High memory usage
- Slow metric collection
- Network timeouts
- Large log files

## 🔧 General Troubleshooting Steps

Before diving into specific guides:

1. **Check the logs** - Enable verbose logging with `--verbose` flag
2. **Validate configuration** - Ensure YAML syntax is correct
3. **Verify permissions** - Check file and directory permissions
4. **Test connectivity** - Verify network access to monitored systems
5. **Check system resources** - Ensure adequate CPU, memory, and disk space

## 🚀 Quick Diagnostic Commands

```bash
# Enable verbose logging
./agent --config-path your-config.yaml --verbose

# Test configuration validity
./agent --config-path your-config.yaml --dry-run

# Check API health
curl http://localhost:8080/health

# View real-time logs
tail -f /var/log/senhub/agent.log
```

## 📞 Getting Help

If you can't find a solution here:

1. Check the [documentation index](../../DOCUMENTATION-INDEX.md)
2. Review configuration examples in the [examples directory](../../examples/)
3. Enable debug logging and review detailed error messages
4. Check for known issues in the project repository

## 🆕 Adding New Troubleshooting Guides

When adding new troubleshooting documentation:
1. Use clear problem descriptions
2. Provide step-by-step solutions
3. Include example commands and outputs
4. Reference related configuration sections