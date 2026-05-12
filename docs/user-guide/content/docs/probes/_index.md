---
title: "Probes"
weight: 10
bookCollapseSection: false
---

# Probes

Detailed documentation for each monitoring probe type supported by SenHub Agent.

### System (Free)

- **[CPU]({{< relref "/docs/probes/cpu" >}})** - Processor utilization and load
- **[Memory]({{< relref "/docs/probes/memory" >}})** - Physical memory and swap usage
- **[Network]({{< relref "/docs/probes/network" >}})** - Network interface bandwidth and errors
- **[Logical Disk]({{< relref "/docs/probes/logicaldisk" >}})** - Disk space, I/O, and filesystem health
- **[Linux Logs]({{< relref "/docs/probes/linux-logs" >}})** - Local systemd journal collection (Linux only)

### Infrastructure (Pro)

- **[Veeam]({{< relref "/docs/probes/veeam" >}})** - Veeam Backup & Replication v13 monitoring
- **[Citrix]({{< relref "/docs/probes/citrix" >}})** - Citrix Virtual Apps and Desktops monitoring
- **[NetScaler]({{< relref "/docs/probes/netscaler" >}})** - Citrix ADC / NetScaler monitoring
- **[Redfish]({{< relref "/docs/probes/redfish" >}})** - Hardware monitoring via Redfish API (Dell iDRAC, HPE iLO)
- **[Syslog]({{< relref "/docs/probes/syslog" >}})** - Syslog message collection (UDP/TCP)
- **[Event]({{< relref "/docs/probes/event" >}})** - Custom event collection via HTTP

### Network (Pro)

- **[Ping WebApp]({{< relref "/docs/probes/ping-webapp" >}})** - Web application availability (ICMP)
- **[Load WebApp]({{< relref "/docs/probes/load-webapp" >}})** - Web page load time measurement
- **[Ping Gateway]({{< relref "/docs/probes/ping-gateway" >}})** - Default gateway connectivity

### Advanced

- **[WiFi Signal]({{< relref "/docs/probes/wifi-signal-strength" >}})** - Wireless signal quality (Pro)
- **[OpenTelemetry]({{< relref "/docs/probes/otel" >}})** - OTLP metrics, traces, and logs (Enterprise)
