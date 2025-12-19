# User Guide

Complete documentation for SenHub Agent users and administrators.

## Complete Documentation

### Installation and Getting Started

1. **[Installation](./INSTALLATION.md)**
   - Windows/Linux/macOS installation
   - HTTP and HTTPS options
   - SSL certificates
   - Verification and uninstallation

2. **[Operating Modes](./OPERATING-MODES.md)**
   - Online vs Offline mode
   - Detailed comparison
   - Switching between modes
   - Use cases per mode

### Configuration

3. **[Agent Configuration](./AGENT-CONFIGURATION.md)**
   - YAML file structure
   - License System (contact support@senhub.io, installation, verification)
   - Auto-update
   - Cache

4. **[HTTP/HTTPS Configuration](./HTTP-HTTPS-CONFIGURATION.md)**
   - HTTP Strategy
   - SSL/TLS certificates
   - Bind address
   - API endpoints

5. **[Probes Configuration](./PROBES-CONFIGURATION.md)**
   - System probes (Free: cpu, memory, disk, network)
   - Network probes (Pro: ping, wifi)
   - Infrastructure probes (Pro/Enterprise: redfish, citrix, netscaler, syslog)
   - Complete configuration examples

### Usage

6. **[Web Interface](./WEB-INTERFACE.md)**
   - Main dashboard and navigation
   - API Explorer (interactive endpoint testing)
   - Metrics Browser (filtering by probe/tag)
   - Probes Status (diagnostics)
   - License Information (detailed status)
   - PRTG Lookups (.ovl download)

7. **[Metrics Usage](./METRICS-USAGE.md)**
   - PRTG integration (XML/REST sensors, lookups)
   - Nagios/Icinga integration (checks, NRPE)
   - Grafana integration (JSON datasource, dashboards)
   - Custom scripts (Python, PowerShell)
   - Examples by probe type

### Troubleshooting

8. **[Troubleshooting](./TROUBLESHOOTING.md)**
   - Logging System (modules, activation, runtime)
   - Common issues
   - Step-by-step solutions

---

## Where to Start?

### New User
1. [Installation](./INSTALLATION.md) - Install the agent
2. [Operating Modes](./OPERATING-MODES.md) - Understand online vs offline
3. [Agent Configuration](./AGENT-CONFIGURATION.md) - Configure the agent

### Advanced Configuration
1. [HTTP/HTTPS](./HTTP-HTTPS-CONFIGURATION.md) - Secure with SSL/TLS
2. [Probes](./PROBES-CONFIGURATION.md) - Add monitoring probes

### Usage and Integrations
1. [Web Interface](./WEB-INTERFACE.md) - Use the dashboard and API Explorer
2. [Metrics Usage](./METRICS-USAGE.md) - Integrate with PRTG/Nagios/Grafana

### In Case of Issues
1. [Troubleshooting](./TROUBLESHOOTING.md) - Diagnostics and logs

---

## Documentation Statistics

- 8 complete documents (~35,000 words, 4,000+ lines)
- 26 Mermaid diagrams (architecture, flows, decision trees)
- 45+ screenshots indicated with detailed descriptions
- Complete license system (request support@senhub.io, JSON format, installation, verification)
- Modular logging system (16 modules, CLI/runtime activation)
- Monitoring integrations (PRTG, Nagios, Grafana with practical examples)
- Integration scripts (Python, PowerShell, Bash)

### Documents Created

1. **INSTALLATION.md** (500 lines) - Multi-platform installation
2. **OPERATING-MODES.md** (400 lines) - Online/Offline modes
3. **AGENT-CONFIGURATION.md** (600 lines) - Agent and license configuration
4. **HTTP-HTTPS-CONFIGURATION.md** (200 lines) - SSL/TLS and security
5. **TROUBLESHOOTING.md** (400 lines) - Troubleshooting and logging
6. **PROBES-CONFIGURATION.md** (500 lines) - All probes configuration
7. **WEB-INTERFACE.md** (650 lines) - Dashboard and API Explorer
8. **METRICS-USAGE.md** (800 lines) - Monitoring integrations

### Microsoft Word Version

A consolidated Word document is available for offline reading and distribution.

**Generate the Word document:**
```bash
cd docs/user-guide
./generate-word-doc.sh
```

**Output:**
- SenHub-Agent-User-Guide-Complete.docx (~173KB)
- Includes all 8 documentation sections
- Table of contents with 3 levels
- Internal cross-reference links (all links work within the document)
- Metadata (title, author, date)

**Requirements:** Pandoc must be installed (`brew install pandoc` on macOS)

**Note:** The script automatically transforms cross-file Markdown links into internal document anchors, ensuring all links work correctly in the consolidated Word format.

---

## License

**License Request**: Contact support@senhub.io

Complete documentation in [AGENT-CONFIGURATION.md](./AGENT-CONFIGURATION.md#license-system)

---

## Support

**Email**: support@senhub.io
**Documentation**: https://docs.senhub.io
**GitHub**: https://github.com/senhub-io/senhub-agent
