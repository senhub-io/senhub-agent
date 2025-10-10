# SenHub Agent - Offline Mode Quick Start Guide

## 🚀 5-Minute Setup

Get your SenHub Agent running in offline mode with comprehensive monitoring in just 5 minutes.

### Step 1: Download and Install

```bash
# Download the latest SenHub Agent
wget https://releases.senhub.io/agent/latest/senhub-agent-linux-amd64
chmod +x senhub-agent-linux-amd64
sudo mv senhub-agent-linux-amd64 /usr/local/bin/agent

# Or use the installer script
curl -sSL https://install.senhub.io/agent | bash
```

### Step 2: Install in Offline Mode

```bash
# Basic installation (HTTP on localhost:8080)
sudo ./agent install --offline

# Recommended: HTTPS installation (secure on port 8443)
sudo ./agent install --offline --enable-https
```

### Step 3: Start the Agent

```bash
# Start the service
sudo ./agent start

# Verify it's running
sudo ./agent status
```

### Step 4: Access Your Dashboard

Open your browser and navigate to:

- **HTTP**: http://localhost:8080/web/{agentkey}/dashboard
- **HTTPS**: https://localhost:8443/web/{agentkey}/dashboard

> **Agent Key**: Check your configuration file `./agent-config.yaml` for the generated agent key.

## 🎯 Common Installation Scenarios

### Scenario 1: Local Development

**Perfect for**: Testing, development, learning

```bash
./agent install --offline
./agent start
```

✅ **Access**: http://localhost:8080/web/{agentkey}/dashboard  
✅ **Security**: Basic (localhost only)  
✅ **Complexity**: Minimal  

### Scenario 2: Internal Network Monitoring

**Perfect for**: Office networks, internal servers

```bash
./agent install --offline --enable-https \
  --https-hosts "agent.company.local,192.168.1.100"
./agent start
```

✅ **Access**: https://agent.company.local:8443/web/{agentkey}/dashboard  
✅ **Security**: HTTPS with auto-certificates  
✅ **Network**: Accessible from internal network  

### Scenario 3: Production Server

**Perfect for**: Production environments, external access

```bash
# First, obtain SSL certificate from your CA
./agent install --offline --enable-https \
  --cert-file /etc/ssl/certs/server.pem \
  --key-file /etc/ssl/private/server.key \
  --https-port 443 \
  --min-tls-version 1.3
./agent start
```

✅ **Access**: https://monitoring.yourdomain.com/web/{agentkey}/dashboard  
✅ **Security**: Production-grade TLS  
✅ **Network**: Secure external access  

### Scenario 4: High-Security Environment

**Perfect for**: Government, finance, compliance

```bash
./agent install --offline --enable-https \
  --https-port 9443 \
  --min-tls-version 1.3 \
  --cert-file /etc/pki/tls/certs/agent.crt \
  --key-file /etc/pki/tls/private/agent.key \
  --config-path /opt/secure/agent-config.yaml
./agent start
```

✅ **Access**: https://secure-host:9443/web/{agentkey}/dashboard  
✅ **Security**: TLS 1.3, custom certificates, non-standard port  
✅ **Compliance**: Meets strict security requirements  

## 🖥️ What You Get Out of the Box

### System Monitoring (Enabled by Default)

- **CPU Usage**: Real-time processor utilization
- **Memory Usage**: RAM and swap utilization  
- **Network Traffic**: Interface statistics and throughput
- **Disk Usage**: Space utilization and I/O metrics

### Web Interface Features

- **📊 Dashboard**: Real-time system overview
- **🔍 API Explorer**: Interactive endpoint testing
- **📚 Documentation**: Complete API reference
- **⚙️ Administration**: Cache and log management

### API Formats

- **PRTG**: Ready for PRTG Network Monitor integration
- **Nagios**: Compatible with Nagios/Icinga monitoring
- **SenHub**: Native format for SenHub platform
- **Prometheus**: Metrics format for Grafana/Prometheus

## 🔧 Quick Configuration

### Find Your Agent Key

```bash
# Check the generated configuration
cat ./agent-config.yaml | grep "key:"
# Output: key: "offline-hostname-1625097600-a1b2c3d4"
```

### Test API Endpoints

```bash
# Replace {agentkey} with your actual key
AGENT_KEY="your-agent-key-here"

# Test CPU metrics (PRTG format)
curl "http://localhost:8080/api/$AGENT_KEY/prtg/metrics/cpu"

# Test system health (Nagios format)  
curl "http://localhost:8080/api/$AGENT_KEY/nagios/check/cpu_usage"
```

### Add More Monitoring

Edit `./agent-config.yaml` to enable additional probes:

```yaml
probes:
  # Current probes...
  
  # Add WiFi monitoring
  - name: wifi_signal_strength
    params: {}
    
  # Add website monitoring
  - name: ping_webapp
    params:
      url: "https://yourwebsite.com"
      
  # Add server hardware monitoring (if applicable)
  - name: redfish
    params:
      endpoint: "https://idrac.server.local"
      username: "monitoring"
      password: "secure-password"
```

Then restart the agent:
```bash
sudo ./agent stop
sudo ./agent start
```

## 📱 Mobile and Remote Access

### Configure for Remote Access

```bash
# Allow external connections with HTTPS
sudo ./agent install --offline --enable-https \
  --https-hosts "$(hostname),$(hostname -I | awk '{print $1}')"
  
# Configure firewall
sudo ufw allow 8443/tcp
```

### Access from Mobile

Navigate to: `https://your-server-ip:8443/web/{agentkey}/dashboard`

> **Note**: For self-signed certificates, you'll need to accept the security warning on first visit.

## 🔗 Integration Quick Start

### PRTG Network Monitor

1. **Add HTTP Advanced Sensor**
2. **URL**: `http://your-server:8080/api/{agentkey}/prtg/metrics/cpu`
3. **Method**: GET
4. **Scanning Interval**: 60 seconds

### Nagios/Icinga

```bash
# Add to commands.cfg
define command {
    command_name    check_senhub_cpu
    command_line    $USER1$/check_http -H $HOSTADDRESS$ -p 8080 \
                    -u "/api/{agentkey}/nagios/check/cpu_usage"
}

# Add to services.cfg
define service {
    host_name               your-server
    service_description     CPU Usage
    check_command          check_senhub_cpu
}
```

### Grafana Dashboard

1. **Add Prometheus Data Source**
2. **URL**: `http://your-server:8080/api/{agentkey}/prometheus/metrics`
3. **Import SenHub Dashboard**: [Download from GitHub](https://github.com/senhub/grafana-dashboards)

## 🆘 Quick Troubleshooting

### Agent Won't Start

```bash
# Check service status
sudo ./agent status

# View logs
sudo ./agent run --offline --verbose

# Check permissions
sudo chown -R root:root /usr/local/bin/agent
sudo chmod +x /usr/local/bin/agent
```

### Can't Access Dashboard

```bash
# Check if agent is listening
sudo netstat -tlnp | grep :8080

# Verify agent key
grep "key:" ./agent-config.yaml

# Test local connection
curl -I http://localhost:8080/health
```

### HTTPS Certificate Issues

```bash
# Check certificate files
ls -la ./certs/

# Regenerate certificates
rm -rf ./certs/
sudo ./agent stop
sudo ./agent run --offline --enable-https

# Test HTTPS connection
curl -k -I https://localhost:8443/health
```

### Firewall Issues

```bash
# Ubuntu/Debian
sudo ufw allow 8080/tcp
sudo ufw allow 8443/tcp

# CentOS/RHEL
sudo firewall-cmd --add-port=8080/tcp --permanent
sudo firewall-cmd --add-port=8443/tcp --permanent
sudo firewall-cmd --reload

# Test connectivity
telnet your-server 8080
```

## 📞 Getting Help

### Check Status
```bash
sudo ./agent status          # Service status
curl localhost:8080/health   # Health check
```

### Enable Debug Mode
```bash
sudo ./agent run --offline --verbose --debug-modules strategy.http,cache
```

### Common Log Locations
- **Linux**: `/var/log/senhub-agent/`
- **systemd**: `journalctl -u senhub-agent`
- **Console**: Direct output when running `./agent run`

### Community Support
- **Documentation**: Full documentation in `OFFLINE-MODE.md`
- **GitHub Issues**: Report bugs and get community help
- **Configuration Examples**: Check `examples/` directory

## 🎓 Next Steps

### Learn More
- **[Complete Offline Guide](OFFLINE-MODE.md)**: Comprehensive documentation
- **[HTTPS Configuration](HTTPS-CONFIGURATION.md)**: Advanced TLS setup
- **[Probe Configuration](PROBE-CONFIGURATION.md)**: Add custom monitoring

### Advanced Features
- **Custom Certificates**: Production-ready TLS setup
- **Hardware Monitoring**: Add Redfish probes for server hardware
- **Log Collection**: Configure syslog and event collection
- **OpenTelemetry**: Integrate with OTEL ecosystem

### Production Deployment
- **Load Balancing**: Deploy multiple agents behind a load balancer
- **High Availability**: Set up agent clustering
- **Security Hardening**: Implement advanced security measures
- **Automation**: Use configuration management tools

---

**🎉 Congratulations!** You now have a fully functional SenHub Agent running in offline mode with comprehensive system monitoring, a web interface, and API integration capabilities.

---

**Last Updated**: January 2025  
**Version**: SenHub Agent v0.8.0+  
**Estimated Setup Time**: 5 minutes