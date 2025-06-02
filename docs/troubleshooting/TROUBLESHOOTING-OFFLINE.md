# SenHub Agent - Offline Mode Troubleshooting Guide

## Common Issues and Solutions

### 🚨 Installation Issues

#### 1. Permission Denied During Installation

**Symptoms:**
```bash
./agent install --offline
Error: open /Library/LaunchDaemons/senhub-agent.plist: permission denied
```

**Solution:**
```bash
# Run with sudo for service installation
sudo ./agent install --offline

# Or install without service registration
./agent run --offline  # No service, just direct execution
```

**Prevention:**
- Always use `sudo` for system service installation
- Use `./agent run --offline` for non-privileged testing

#### 2. Configuration File Not Generated

**Symptoms:**
- Agent starts but no `agent-config.yaml` file is created
- Dashboard shows no agent key

**Diagnosis:**
```bash
# Check current directory permissions
ls -la ./
touch test-file && rm test-file  # Test write permissions

# Check if agent is actually running in offline mode
ps aux | grep agent
```

**Solution:**
```bash
# Ensure write permissions to current directory
chmod 755 .
sudo chown $USER:$USER .

# Verify offline mode is enabled
./agent run --offline --verbose --config-path ./debug-config.yaml
```

#### 3. Binary Not Found or Executable

**Symptoms:**
```bash
./agent: No such file or directory
# or
./agent: Permission denied
```

**Solution:**
```bash
# Make binary executable
chmod +x ./agent

# Verify binary
file ./agent
./agent version

# If not found, download again
wget https://releases.senhub.io/agent/latest/senhub-agent-$(uname -s)-$(uname -m)
```

### 🌐 Network and Connectivity Issues

#### 1. Cannot Access Dashboard

**Symptoms:**
- Browser shows "This site can't be reached"
- Connection refused or timeout

**Diagnosis:**
```bash
# Check if agent is running
sudo ./agent status

# Check if port is listening
sudo netstat -tlnp | grep :8080  # For HTTP
sudo netstat -tlnp | grep :8443  # For HTTPS

# Test local connection
curl -I http://localhost:8080/health
curl -k -I https://localhost:8443/health  # For HTTPS with self-signed cert
```

**Solutions:**

**If agent is not running:**
```bash
sudo ./agent start
sudo ./agent status
```

**If port is not listening:**
```bash
# Check configuration
grep -A 5 "port:" ./agent-config.yaml

# Check for port conflicts
sudo lsof -i :8080
sudo lsof -i :8443

# Use different port
./agent run --offline --https-port 9443
```

**If firewall is blocking:**
```bash
# Ubuntu/Debian
sudo ufw allow 8080/tcp
sudo ufw allow 8443/tcp

# CentOS/RHEL
sudo firewall-cmd --add-port=8080/tcp --permanent
sudo firewall-cmd --add-port=8443/tcp --permanent
sudo firewall-cmd --reload

# Test connectivity
telnet localhost 8080
telnet localhost 8443
```

#### 2. External Access Not Working

**Symptoms:**
- Dashboard works on localhost but not from other machines
- "Connection refused" from remote hosts

**Diagnosis:**
```bash
# Check bind address in configuration
grep -A 3 "bind_address" ./agent-config.yaml

# Check what interface agent is listening on
sudo netstat -tlnp | grep agent
```

**Solution:**
```bash
# For HTTP mode, agent binds to 127.0.0.1 (localhost only)
# Enable HTTPS for external access
sudo ./agent stop
sudo ./agent install --offline --enable-https
sudo ./agent start

# Or manually edit configuration
# Change bind_address from "127.0.0.1" to "0.0.0.0"
sed -i 's/bind_address: "127.0.0.1"/bind_address: "0.0.0.0"/' ./agent-config.yaml
sudo ./agent restart
```

#### 3. DNS Resolution Issues

**Symptoms:**
- HTTPS certificate warnings for hostname mismatch
- Cannot access via hostname

**Solution:**
```bash
# Regenerate certificate with correct hostnames
sudo ./agent stop
./agent install --offline --enable-https \
  --https-hosts "$(hostname),$(hostname -f),$(hostname -I | awk '{print $1}')"
sudo ./agent start

# Or add to /etc/hosts on client machines
echo "192.168.1.100 agent.company.local" | sudo tee -a /etc/hosts
```

### 🔒 HTTPS and Certificate Issues

#### 1. Certificate Not Found

**Symptoms:**
```bash
ERR Failed to configure TLS error="auto-generated certificate not found at ./certs/agent-cert.pem"
```

**Solution:**
```bash
# Create certs directory
mkdir -p ./certs

# Regenerate certificates by running agent
./agent run --offline --enable-https --verbose

# Check certificate files
ls -la ./certs/
openssl x509 -in ./certs/agent-cert.pem -text -noout
```

#### 2. Permission Denied on Certificate Files

**Symptoms:**
```bash
ERR Failed to load TLS certificate error="permission denied"
```

**Solution:**
```bash
# Fix certificate file permissions
sudo chown root:root ./certs/agent-*.pem
chmod 644 ./certs/agent-cert.pem
chmod 600 ./certs/agent-key.pem

# For custom certificates
sudo chown root:root /path/to/cert.pem /path/to/key.pem
chmod 644 /path/to/cert.pem
chmod 600 /path/to/key.pem
```

#### 3. Certificate/Key Mismatch

**Symptoms:**
```bash
ERR Failed to load TLS certificate error="private key does not match public key"
```

**Diagnosis:**
```bash
# Verify certificate and key match
cert_mod=$(openssl x509 -noout -modulus -in cert.pem | openssl md5)
key_mod=$(openssl rsa -noout -modulus -in key.pem | openssl md5)
echo "Cert: $cert_mod"
echo "Key:  $key_mod"
```

**Solution:**
```bash
# If using auto-generated certificates, regenerate
rm -rf ./certs/
./agent run --offline --enable-https

# If using custom certificates, verify files
openssl x509 -in /path/to/cert.pem -text -noout
openssl rsa -in /path/to/key.pem -check -noout
```

#### 4. Browser Certificate Warnings

**Symptoms:**
- "Your connection is not private" warning
- "NET::ERR_CERT_AUTHORITY_INVALID" error

**For Development/Testing:**
```bash
# Click "Advanced" → "Proceed to localhost (unsafe)" in browser
# This is normal for self-signed certificates
```

**For Production:**
```bash
# Option 1: Use valid certificates from CA
./agent install --offline --enable-https \
  --cert-file /etc/ssl/certs/valid-cert.pem \
  --key-file /etc/ssl/private/valid-key.pem

# Option 2: Add self-signed certificate to browser trust store
# Export certificate
openssl x509 -outform DER -in ./certs/agent-cert.pem -out agent-cert.der
# Import into browser certificate store (varies by browser/OS)
```

### 📊 Monitoring and API Issues

#### 1. No Metrics Data

**Symptoms:**
- Dashboard shows no data
- API endpoints return empty responses

**Diagnosis:**
```bash
# Check if probes are running
curl "http://localhost:8080/api/{agentkey}/info/probes"

# Check probe status in logs
./agent run --offline --verbose --debug-modules probe.cpu,probe.memory

# Test individual probe endpoints
curl "http://localhost:8080/api/{agentkey}/senhub/metrics/cpu"
curl "http://localhost:8080/api/{agentkey}/prtg/metrics/memory"
```

**Solution:**
```bash
# Verify agent key in URL
grep "key:" ./agent-config.yaml

# Check probe configuration
grep -A 10 "probes:" ./agent-config.yaml

# Restart agent to reload configuration
sudo ./agent restart
```

#### 2. API Authentication Errors

**Symptoms:**
```bash
curl http://localhost:8080/api/wrong-key/metrics/cpu
# Returns: 404 Not Found or 401 Unauthorized
```

**Solution:**
```bash
# Get correct agent key
AGENT_KEY=$(grep "key:" ./agent-config.yaml | cut -d'"' -f2)
echo "Agent key: $AGENT_KEY"

# Test with correct key
curl "http://localhost:8080/api/$AGENT_KEY/health"
```

#### 3. Slow or Timeout Responses

**Symptoms:**
- API requests take longer than 30 seconds
- Browser dashboard loads slowly

**Diagnosis:**
```bash
# Check system resources
top
free -h
df -h

# Test API response time
time curl "http://localhost:8080/api/{agentkey}/prtg/metrics/cpu"

# Check cache status
curl "http://localhost:8080/api/{agentkey}/admin/cache"
```

**Solution:**
```bash
# Increase probe intervals to reduce load
# Edit agent-config.yaml
sed -i 's/interval: 30/interval: 60/' ./agent-config.yaml
sudo ./agent restart

# Clear cache if corrupted
curl -X DELETE "http://localhost:8080/api/{agentkey}/admin/cache"
```

### 🔧 Configuration Issues

#### 1. Invalid YAML Syntax

**Symptoms:**
```bash
ERR Failed to parse YAML config error="yaml: line 15: did not find expected key"
```

**Solution:**
```bash
# Validate YAML syntax
python -c "import yaml; yaml.safe_load(open('./agent-config.yaml'))"

# Or use online YAML validator
# Check indentation (use spaces, not tabs)
# Check quotes and special characters

# Regenerate configuration if corrupted
mv agent-config.yaml agent-config.yaml.backup
./agent run --offline  # Generates new configuration
```

#### 2. Probe Configuration Errors

**Symptoms:**
```bash
ERR Error starting probe error="invalid probe parameters"
```

**Diagnosis:**
```bash
# Check probe configuration syntax
./agent run --offline --verbose --debug-modules probe.cpu,probe.memory,configuration

# Validate specific probe
grep -A 5 "name: cpu" ./agent-config.yaml
```

**Solution:**
```bash
# Fix common probe configuration issues:

# CPU probe (minimal config)
- name: cpu
  params:
    interval: 30

# Memory probe (minimal config)  
- name: memory
  params:
    interval: 30

# Network probe (minimal config)
- name: network
  params:
    interval: 60

# WebApp probe (requires URL)
- name: ping_webapp
  params:
    url: "https://example.com"
    timeout: 30
```

#### 3. Storage Strategy Errors

**Symptoms:**
```bash
ERR Failed to start strategy error="invalid storage configuration"
```

**Solution:**
```bash
# Verify HTTP strategy configuration
grep -A 10 "name: http" ./agent-config.yaml

# Standard HTTP strategy configuration:
storage:
  - name: http
    params:
      port: 8080
      bind_address: "127.0.0.1"
      endpoints: ["prtg", "senhub", "web", "nagios"]
```

### 🖥️ System-Specific Issues

#### Linux Issues

**systemd Service Problems:**
```bash
# Check service status
systemctl status senhub-agent

# View detailed logs
journalctl -u senhub-agent -f

# Restart service
sudo systemctl restart senhub-agent

# If service fails to start
sudo systemctl edit senhub-agent
# Add:
[Service]
Type=simple
Restart=always
```

**SELinux Issues (CentOS/RHEL):**
```bash
# Check if SELinux is blocking
sudo ausearch -m AVC -ts recent | grep agent

# Set permissive mode for testing
sudo setenforce 0

# Create custom SELinux policy (production)
sudo setsebool -P httpd_can_network_connect 1
```

#### Windows Issues

**Service Registration:**
```cmd
# Run as Administrator
agent.exe install --offline --enable-https

# Check Windows Service
sc query senhub-agent

# View Event Logs
eventvwr.msc
# Navigate to: Applications and Services Logs → SenHub Agent
```

**Firewall Configuration:**
```cmd
# Allow through Windows Firewall
netsh advfirewall firewall add rule name="SenHub Agent HTTP" dir=in action=allow protocol=TCP localport=8080
netsh advfirewall firewall add rule name="SenHub Agent HTTPS" dir=in action=allow protocol=TCP localport=8443
```

#### macOS Issues

**Gatekeeper Warnings:**
```bash
# Allow unsigned binary
sudo spctl --master-disable
./agent run --offline
sudo spctl --master-enable

# Or sign the binary (for distribution)
codesign -s "Developer ID" ./agent
```

**LaunchDaemon Permissions:**
```bash
# Fix LaunchDaemon permissions
sudo chown root:wheel /Library/LaunchDaemons/senhub-agent.plist
sudo chmod 644 /Library/LaunchDaemons/senhub-agent.plist
```

### 🔍 Debug Mode and Logging

#### Enable Comprehensive Debugging

```bash
# Full debug mode
./agent run --offline --verbose

# Specific module debugging
./agent run --offline --debug-modules strategy.http,cache,configuration,probe.cpu

# HTTP strategy specific debugging
./agent run --offline --debug-modules strategy.http
```

#### Log Analysis

```bash
# Real-time log monitoring
tail -f /var/log/senhub-agent/agent.log

# Search for specific errors
grep -i "error\|failed\|err" /var/log/senhub-agent/agent.log

# Check certificate-related issues
grep -i "tls\|certificate\|ssl" /var/log/senhub-agent/agent.log

# Monitor probe activity
grep -i "probe" /var/log/senhub-agent/agent.log
```

#### Log Permission Issues

**Symptoms:**
```
zerolog: could not write event: can't rename log file: permission denied
```

**Cause:**
The agent tries to write logs to system directories (`/Library/Logs/SenHub` on macOS, `/var/log/senhub` on Linux) but lacks the necessary permissions.

**Solution:**
The agent automatically detects permission issues and falls back to a local directory. No action needed - this is expected behavior when running without elevated privileges.

**Verification:**
```bash
# Check where logs are actually being written
./agent run --offline --verbose 2>&1 | grep "Using log file"

# The output will show the actual log location, e.g.:
# Using log file: /Users/username/agent-directory/senhubagent.log
```

**Note:** If you prefer system-wide logging, run the agent with appropriate permissions or install as a service.

#### Performance Monitoring

```bash
# Monitor resource usage
top -p $(pgrep agent)

# Monitor network connections
sudo netstat -tlnp | grep agent

# Monitor file descriptors
sudo lsof -p $(pgrep agent)

# Monitor disk I/O
sudo iotop -p $(pgrep agent)
```

### 📞 Getting Help

#### Collect Diagnostic Information

```bash
#!/bin/bash
# Create diagnostic report
echo "=== SenHub Agent Diagnostic Report ===" > diagnostic.txt
echo "Date: $(date)" >> diagnostic.txt
echo "System: $(uname -a)" >> diagnostic.txt
echo "Agent Version: $(./agent version)" >> diagnostic.txt
echo "" >> diagnostic.txt

echo "=== Configuration ===" >> diagnostic.txt
cat ./agent-config.yaml >> diagnostic.txt
echo "" >> diagnostic.txt

echo "=== Service Status ===" >> diagnostic.txt
sudo ./agent status >> diagnostic.txt 2>&1
echo "" >> diagnostic.txt

echo "=== Network Status ===" >> diagnostic.txt
sudo netstat -tlnp | grep -E ":8080|:8443" >> diagnostic.txt
echo "" >> diagnostic.txt

echo "=== Certificate Info ===" >> diagnostic.txt
if [ -f "./certs/agent-cert.pem" ]; then
    openssl x509 -in ./certs/agent-cert.pem -text -noout >> diagnostic.txt
fi
echo "" >> diagnostic.txt

echo "=== Recent Logs ===" >> diagnostic.txt
tail -50 /var/log/senhub-agent/agent.log >> diagnostic.txt 2>/dev/null

echo "Diagnostic report saved to diagnostic.txt"
```

#### Contact Support

When contacting support, include:

1. **Diagnostic report** (generated above)
2. **Error messages** (exact text)
3. **Steps to reproduce** the issue
4. **System information** (OS, version, architecture)
5. **Installation method** used
6. **Configuration file** (remove sensitive information)

#### Community Resources

- **GitHub Issues**: [SenHub Agent Repository](https://github.com/senhub/agent/issues)
- **Documentation**: `OFFLINE-MODE.md`, `HTTPS-CONFIGURATION.md`
- **Examples**: Check `examples/` directory in repository
- **Stack Overflow**: Tag questions with `senhub-agent`

---

**Last Updated**: January 2025  
**Version**: SenHub Agent v0.8.0+  
**Troubleshooting Level**: Comprehensive