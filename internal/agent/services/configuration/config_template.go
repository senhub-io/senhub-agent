package configuration

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
#     base_url: "https://citrix-director.company.com"  # REQUIRED (API path added automatically)
#
#     # Optional: Delivery Controller for site filtering
#     delivery_controller:
#       url: "https://citrix-ddc.company.com"
#       fallback_urls:
#         - "https://citrix-ddc-backup.company.com"
#       site_filter: "SITE-NAME"  # Only monitor this site
#
#     interval: 120               # Optional, default: 120s (2min)
#
#     auth:
#       # Authentication methods are automatic: NTLM for Director, Basic for DDC
#       username: "DOMAIN\\user"  # REQUIRED
#       password: "password"      # REQUIRED
#
#     tls:
#       verify_ssl: true          # Optional, default: true
#
#     timeout: 30                 # Optional, default: 30s
#     retry:
#       max_attempts: 3           # Optional, default: 3
#       backoff_factor: 2.0       # Optional, default: 2.0

# # Syslog event collection
# - name: syslog           # Display name
#   type: syslog           # Probe type
#   params:
#     port: 514        # Optional, default: 514, range: 1-65535
#     protocol: "udp"  # Optional, default: "udp", values: "tcp"/"udp"

# # Custom events endpoint (POST /event)
# - name: event            # Display name
#   type: event            # Probe type
#   params:
#     address: "127.0.0.1"  # Optional, default: "127.0.0.1"
#     port: 5656            # Optional, default: 5656, range: 1-65535
#     protocol: "tcp"       # Optional, default: "tcp", values: "tcp"/"udp"

# # OpenTelemetry collector
# - name: otel             # Display name
#   type: otel             # Probe type
#   params:
#     endpoint: "http://localhost:4318"  # REQUIRED
#     name: "otel"                       # Optional, default: "otel"
#     interval: 60                       # Optional, default: 60s
#     protocol: "http"                   # Optional, auto-detected ("http"/"grpc")
#     telemetry_types:                   # Optional, default: all
#       - metrics
#       - traces
#       - logs
#     headers:                           # Optional, HTTP only
#       Authorization: "Bearer token123"
#     insecure: false                    # Optional, gRPC only
`
