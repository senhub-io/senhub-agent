package configuration

// LicenseDocumentationTemplate contains license configuration documentation.
// This template is shared between offline and online configuration generation.
const LicenseDocumentationTemplate = `
  # license: ""  # Optional: License token for paid probes (leave empty for free tier)
  #
  # ===== LICENSE TIERS =====
  # FREE TIER (no license required):
  #   - cpu, memory, logicaldisk, network
  #   - http/https endpoints (prtg, nagios, web)
  #   - offline mode
  #
  # PAID PROBES (license required):
  #   - redfish, citrix, syslog
  #   - ping_webapp, load_webapp, ping_gateway, wifi_signal_strength
  #
  # ===== LICENSE FORMAT =====
  # License uses JSON format with the following fields:
  #
  # Pro License (specific probes):
  # license: |
  #   {
  #     "tier": "pro",
  #     "authorized_probes": ["redfish", "citrix"],
  #     "expires_at": "2025-12-31T23:59:59Z",
  #     "issued_at": "2025-01-01T00:00:00Z",
  #     "subject": "customer-id"
  #   }
  #
  # Enterprise License (all probes):
  # license: |
  #   {
  #     "tier": "enterprise",
  #     "authorized_probes": ["*"],
  #     "expires_at": "2026-12-31T23:59:59Z",
  #     "issued_at": "2025-01-01T00:00:00Z",
  #     "subject": "customer-id"
  #   }
  #
  # ===== GRACE PERIOD =====
  # Expired licenses have a 7-day grace period before being disabled.
  # During grace period, all authorized probes remain functional.
`

// AgentYAMLTemplate is the globals-only top-level file written by a
// fresh `agent install` (0.2.x+ default layout). Probes and storage
// strategies live in sibling probes.d/ and strategies.d/ directories
// — see HostProbesFragmentTemplate and HTTPStrategyFragmentTemplate.
//
// Sprintf args:
//
//  1. config_version (int)         %d
//  2. agent version string (str)   %s
//  3. timestamp (str)              %s
//  4. config_version (int, body)   %d
//  5. agent.key (UUID str)         %s
//  6. license documentation block  %s — LicenseDocumentationTemplate
//  7. auto_update.enabled (bool)   %t
//  8. auto_update.include_beta     %t
//  9. auto_update.url (str)        %s
//  10. cache.retention_minutes     %d
const AgentYAMLTemplate = `# SenHub Agent — global configuration (multi-file layout)
# Configuration Version: %d (automatically managed)
# Agent Version: %s
# Generated: %s
#
# This file holds ONLY the global blocks (agent, auto_update, cache).
# Probes live in probes.d/ — each fragment is a YAML array of probe
# configs. Storage strategies live in strategies.d/ — one file per
# strategy, with exactly one top-level key (the strategy name).
#
# DO NOT modify config_version manually — it is managed by the agent.

config_version: %d

agent:
  key: "%s"
  mode: offline
  # license: ""  # Uncomment and add your license token here
%s

auto_update:
  enabled: %t           # Enable/disable automatic updates
  include_beta: %t      # Include beta versions in update checks
  url: "%s"             # Update server URL

cache:
  retention_minutes: %d  # Cache retention time in minutes
`

// HostProbesFragmentTemplate is the default probes.d/00-host.yaml
// written by a fresh install — host-local observability that is
// useful on any agent (cpu, memory, network, logicaldisk). Operators
// add more probes by creating new fragments (e.g. 10-mydb.yaml). The
// loader picks up every *.yaml file in alphabetical order; rename to
// *.disabled to opt out without deleting.
const HostProbesFragmentTemplate = `# Default host monitoring probes (multi-file layout).
# Add new probes by creating another file in this directory
# (e.g. 10-mysql.yaml). Files load alphabetically; each is a YAML
# array of probe configs. Disable a fragment by renaming it to
# *.disabled — no deletion required.

- name: cpu              # Display name (free choice)
  type: cpu              # Probe type (technical identifier — must match registered probe)
  params:
    interval: 30

- name: memory           # Display name
  type: memory           # Probe type
  params:
    interval: 30

- name: network          # Display name
  type: network          # Probe type
  params:
    interval: 60

- name: logicaldisk      # Display name
  type: logicaldisk      # Probe type
  params:
    interval: 30
`

// HTTPStrategyFragmentTemplate is the default strategies.d/00-http.yaml.
// The HTTP strategy exposes PRTG / Nagios / Web UI endpoints. Each
// fragment in strategies.d/ MUST carry exactly one top-level key
// (the strategy name). Add another strategy by creating a new file
// (e.g. 10-otlp.yaml containing `otlp:\n  endpoint: ...`).
//
// Sprintf args:
//
//  1. port (int)            %d
//  2. bind_address (str)    %s
//  3. endpoints (str list)  %s — already formatted: "prtg", "web", ...
//  4. TLS section (str)     %s — empty when HTTPS is not enabled
const HTTPStrategyFragmentTemplate = `# Default HTTP strategy — exposes PRTG / Nagios / Web UI endpoints.
# Each file in strategies.d/ MUST have exactly ONE top-level key (the
# strategy name). Add other strategies (otlp, prtg, ...) by creating
# new files. Disable a strategy by renaming the file to *.disabled.

http:
  port: %d
  bind_address: "%s"
  endpoints: [%s]
%s`

// ProbeExamplesTemplate contains commented configuration examples for all available probes.
// This template is shared between offline (LocalConfiguration) and online (RemoteConfiguration)
// modes to ensure consistency and avoid duplication.
//
// When adding a new probe type, add its configuration example here and it will automatically
// appear in both offline and online generated configuration files.
const ProbeExamplesTemplate = `
# ===== CONFIGURATION EXAMPLES (COMMENTED) =====
# Uncomment and configure the probes you need below.
# Remember: 'name' is the display name (free choice), 'type' is the probe type.

# # Network connectivity monitoring
# - name: ping_gateway    # Display name
#   type: ping_gateway    # Probe type
#   params: {}  # Auto-detects gateway
#
# - name: ping_webapp     # Display name
#   type: ping_webapp     # Probe type
#   params:
#     url: "https://example.com"  # REQUIRED
#
# - name: load_webapp     # Display name
#   type: load_webapp     # Probe type
#   params:
#     url: "https://example.com"  # REQUIRED
#     timeout: 30                 # Optional, 1-300s, default: 30s

# # WiFi signal strength (auto-detects if WiFi available)
# - name: wifi_signal_strength   # Display name
#   type: wifi_signal_strength   # Probe type
#   params: {}

# # Server hardware via Redfish (iDRAC, iLO, etc.)
# - name: redfish              # Display name (example: "Production iDRAC")
#   type: redfish              # Probe type
#   params:
#     endpoint: "https://idrac.example.com"  # REQUIRED
#     username: "admin"                      # REQUIRED
#     password: "password123"                # REQUIRED
#     interval: 300                          # Optional, default: 300s (5min)
#     verify_ssl: true                       # Optional, default: true
#     collections:                           # Optional, default: all
#       - system     # General system info
#       - thermal    # Temperatures, fans
#       - power      # Power supply, consumption
#       - processor  # CPU hardware
#       - memory     # RAM hardware
#       - storage    # RAID, disks
#       - drives     # Individual drives
#       - networkadapter  # Network cards

# # Citrix Virtual Apps and Desktops monitoring
# - name: citrix                         # Display name (example: "Production Citrix")
#   type: citrix                         # Probe type
#   params:
#     interval: 120               # Optional, default: 120s (2min)
#     timeout: 30                 # Optional, default: 30s
#
#     # Director/OData API (REQUIRED)
#     # Auth method auto-detected: NTLM
#     director:
#       url: "https://citrix-director.company.com"  # REQUIRED
#       verify_ssl: true          # Optional, default: true
#       auth:
#         username: "DOMAIN\\user"  # REQUIRED
#         password: "password"      # REQUIRED
#
#     # Optional: Delivery Controller for site filtering
#     # Auth method auto-detected: Basic
#     # delivery_controller:
#     #   url: "https://citrix-ddc.company.com"
#     #   fallback_urls:
#     #     - "https://citrix-ddc-backup.company.com"
#     #   verify_ssl: true
#     #   site_filter: "SITE-NAME"
#     #   auth:
#     #     username: "DOMAIN\\ddc-user"   # Inherits from director if omitted
#     #     password: "ddc-password"
#
#     # Optional: License Server for license usage monitoring
#     # Auth method auto-detected: Basic
#     # license_server:
#     #   url: "https://citrix-license-server:8083"
#     #   verify_ssl: true
#     #   auth:
#     #     username: "DOMAIN\\lic-user"   # Inherits from director if omitted
#     #     password: "lic-password"
#
#     retry:
#       max_attempts: 3           # Optional, default: 3
#       backoff_factor: 2.0       # Optional, default: 2.0

# # Veeam Backup & Replication monitoring
# - name: veeam-prod              # Display name
#   type: veeam                   # Probe type
#   params:
#     endpoint: "https://veeam-server"  # REQUIRED: Veeam server hostname or IP
#     port: 9419                        # Optional, default: 9419
#     username: "DOMAIN\\svc_monitoring" # REQUIRED: Veeam Backup Administrator account
#     password: "password"              # REQUIRED
#     interval: 300                     # Optional, default: 300s (5min)
#     verify_ssl: false                 # Optional, default: true
#     hours_to_check: 24               # Optional, default: 24 (job history window)

# # Syslog event collection
# - name: syslog           # Display name
#   type: syslog           # Probe type
#   params:
#     port: 514        # Optional, default: 514, range: 1-65535
#     protocol: "udp"  # Optional, default: "udp", values: "tcp"/"udp"
`
