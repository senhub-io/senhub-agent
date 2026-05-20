# HTTPS/TLS Configuration Guide

## Overview

The SenHub Agent supports comprehensive HTTPS/TLS configuration for secure monitoring. This guide covers all aspects of TLS setup, from auto-generated certificates to production-ready configurations.

## TLS Modes

### 1. Disabled (Default HTTP)

**Use Case**: Local development, localhost-only access
**Security**: Basic HTTP, no encryption
**Configuration**: No TLS parameters needed

```bash
./agent install
# Access: http://localhost:8080/web/{agentkey}/dashboard
```

### 2. Auto-Generated Certificates

**Use Case**: Quick HTTPS setup, internal networks, testing
**Security**: TLS encryption with self-signed certificates
**Configuration**: Automatic certificate generation

```bash
./agent install --enable-https
# Access: https://localhost:8443/web/{agentkey}/dashboard
```

### 3. Provided Certificates

**Use Case**: Production environments, valid CA certificates
**Security**: Full TLS with trusted certificates
**Configuration**: User-provided certificate files

```bash
./agent install --enable-https \
  --cert-file /path/to/cert.pem \
  --key-file /path/to/key.pem
# Access: https://your-domain.com:8443/web/{agentkey}/dashboard
```

## Certificate Generation

### Automatic Generation Process

When using `--enable-https` without custom certificates:

1. **RSA Key Generation**: 2048-bit RSA private key
2. **Certificate Creation**: X.509 certificate with proper extensions
3. **SAN Configuration**: Subject Alternative Names for multiple hostnames
4. **File Storage**: Secure storage in `./certs/` directory
5. **Permission Setting**: Restricted access (600 for private key)

### Certificate Properties

```
Subject: CN=localhost, O=SenHub Agent
Issuer: Self-signed
Validity: 365 days from generation
Key Usage: Digital Signature, Key Encipherment
Extended Key Usage: Server Authentication
Subject Alternative Names: DNS:localhost, IP:127.0.0.1, [custom hosts]
```

### Custom Subject Alternative Names

```bash
# Multiple hostnames for certificate
./agent install --enable-https \
  --https-hosts "agent.company.com,192.168.1.100,monitoring.local,10.0.0.50"
```

This generates a certificate valid for:
- `agent.company.com`
- `192.168.1.100`
- `monitoring.local`
- `10.0.0.50`
- `localhost` (always included)
- `127.0.0.1` (always included)

## Configuration File TLS Section

### Auto-Generated Certificates

```yaml
storage:
  - name: http
    params:
      port: 8443
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "senhub", "web", "nagios"]
      tls:
        enabled: true
        mode: "auto"
        auto_cert:
          organization: "SenHub Agent"
          common_name: "localhost"
          san_hosts: 
            - "localhost"
            - "127.0.0.1"
            - "agent.company.com"
            - "192.168.1.100"
          validity_days: 365
          key_size: 2048
        min_tls_version: "1.2"
        cipher_suites: []  # Empty = secure defaults
```

### Provided Certificates

```yaml
storage:
  - name: http
    params:
      port: 8443
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "senhub", "web", "nagios"]
      tls:
        enabled: true
        mode: "provided"
        cert_file: "/etc/ssl/certs/agent.pem"
        key_file: "/etc/ssl/private/agent.key"
        min_tls_version: "1.3"
        cipher_suites:
          - "TLS_AES_256_GCM_SHA384"
          - "TLS_CHACHA20_POLY1305_SHA256"
          - "TLS_AES_128_GCM_SHA256"
```

## Production Certificate Setup

### Let's Encrypt Integration

#### 1. Generate Let's Encrypt Certificate

```bash
# Using certbot
certbot certonly --standalone \
  -d agent.company.com \
  -d monitoring.company.com \
  --email admin@company.com \
  --agree-tos \
  --non-interactive

# Certificates generated in /etc/letsencrypt/live/agent.company.com/
```

#### 2. Configure Agent

```bash
./agent install --enable-https \
  --cert-file /etc/letsencrypt/live/agent.company.com/fullchain.pem \
  --key-file /etc/letsencrypt/live/agent.company.com/privkey.pem \
  --https-port 443 \
  --min-tls-version 1.3
```

#### 3. Auto-Renewal Setup

```bash
# Add to crontab for automatic renewal
0 12 * * * /usr/bin/certbot renew --quiet --deploy-hook "systemctl restart senhub-agent"
```

### Corporate CA Integration

#### 1. Generate Certificate Request

```bash
# Create private key
openssl genrsa -out agent.key 2048

# Create certificate signing request
openssl req -new -key agent.key -out agent.csr -subj "/CN=agent.company.com/O=Company Name/C=US" \
  -addext "subjectAltName=DNS:agent.company.com,DNS:monitoring.company.com,IP:192.168.1.100"
```

#### 2. Submit to Corporate CA

Submit `agent.csr` to your certificate authority and receive signed certificate.

#### 3. Configure Agent

```bash
./agent install --enable-https \
  --cert-file /etc/ssl/company/agent.crt \
  --key-file /etc/ssl/company/agent.key \
  --https-port 8443 \
  --min-tls-version 1.2
```

## TLS Security Configuration

### Minimum TLS Versions

#### TLS 1.2 (Default)
- **Compatibility**: Supports older systems
- **Security**: Strong encryption, widely supported
- **Use Case**: General purpose, mixed environments

```bash
./agent install --enable-https --min-tls-version 1.2
```

#### TLS 1.3 (Recommended)
- **Compatibility**: Modern systems only
- **Security**: Latest encryption, improved performance
- **Use Case**: High-security environments, modern infrastructure

```bash
./agent install --enable-https --min-tls-version 1.3
```

### Cipher Suite Configuration

#### Default (Secure)
The agent uses secure defaults when no cipher suites are specified:

```yaml
cipher_suites: []  # Uses Go's default secure cipher suites
```

#### Custom Configuration
For compliance or specific security requirements:

```yaml
tls:
  cipher_suites:
    # TLS 1.3 (if min_tls_version: "1.3")
    - "TLS_AES_256_GCM_SHA384"
    - "TLS_CHACHA20_POLY1305_SHA256"
    - "TLS_AES_128_GCM_SHA256"
    
    # TLS 1.2 compatible
    - "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384"
    - "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
    - "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305"
    - "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305"
    - "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
    - "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"
```

## Certificate Management

### Certificate Validation

#### Check Certificate Details
```bash
# View certificate information
openssl x509 -in ./certs/agent-cert.pem -text -noout

# Check certificate validity
openssl x509 -in ./certs/agent-cert.pem -checkend 86400  # Check if expires in 24h

# Verify certificate chain
openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt ./certs/agent-cert.pem
```

#### Test TLS Connection
```bash
# Test TLS handshake
openssl s_client -connect localhost:8443 -servername localhost

# Test with specific TLS version
openssl s_client -connect localhost:8443 -tls1_3

# Test cipher suites
nmap --script ssl-enum-ciphers -p 8443 localhost
```

### Automatic Renewal

#### Self-Signed Certificates
The agent automatically renews self-signed certificates when they expire within 30 days:

```go
// Auto-renewal check at startup
if certificate.NotAfter.Sub(time.Now()) < 30*24*time.Hour {
    regenerateCertificate()
}
```

#### External Certificates
For Let's Encrypt or CA certificates, set up external renewal:

```bash
# Systemd timer for Let's Encrypt renewal
cat > /etc/systemd/system/agent-cert-renew.timer << EOF
[Unit]
Description=Renew Agent TLS Certificate

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
EOF

# Service to restart agent after renewal
cat > /etc/systemd/system/agent-cert-renew.service << EOF
[Unit]
Description=Renew Agent Certificate
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/bin/certbot renew --quiet
ExecStartPost=/bin/systemctl restart senhub-agent
EOF

# Enable timer
systemctl enable agent-cert-renew.timer
systemctl start agent-cert-renew.timer
```

## Network Configuration

### Firewall Rules

#### HTTP Mode (Port 8080)
```bash
# UFW (Ubuntu)
ufw allow 8080/tcp comment "SenHub Agent HTTP"

# iptables
iptables -A INPUT -p tcp --dport 8080 -j ACCEPT

# Windows Firewall
netsh advfirewall firewall add rule name="SenHub Agent HTTP" dir=in action=allow protocol=TCP localport=8080
```

#### HTTPS Mode (Port 8443)
```bash
# UFW (Ubuntu)
ufw allow 8443/tcp comment "SenHub Agent HTTPS"

# iptables
iptables -A INPUT -p tcp --dport 8443 -j ACCEPT

# Windows Firewall
netsh advfirewall firewall add rule name="SenHub Agent HTTPS" dir=in action=allow protocol=TCP localport=8443
```

### Reverse Proxy Configuration

#### Nginx
```nginx
server {
    listen 443 ssl http2;
    server_name monitoring.company.com;
    
    ssl_certificate /etc/ssl/certs/company.crt;
    ssl_private_key /etc/ssl/private/company.key;
    
    # Modern TLS configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE+AESGCM:ECDHE+CHACHA20:DHE+AESGCM:DHE+CHACHA20:!aNULL:!MD5:!DSS;
    ssl_prefer_server_ciphers off;
    
    location / {
        proxy_pass https://127.0.0.1:8443;
        proxy_ssl_verify off;  # For self-signed backend
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

#### Apache
```apache
<VirtualHost *:443>
    ServerName monitoring.company.com
    
    SSLEngine on
    SSLCertificateFile /etc/ssl/certs/company.crt
    SSLCertificateKeyFile /etc/ssl/private/company.key
    
    # Modern TLS configuration
    SSLProtocol all -SSLv3 -TLSv1 -TLSv1.1
    SSLCipherSuite ECDHE+AESGCM:ECDHE+CHACHA20:DHE+AESGCM:DHE+CHACHA20:!aNULL:!MD5:!DSS
    SSLHonorCipherOrder off
    
    ProxyPass / https://127.0.0.1:8443/
    ProxyPassReverse / https://127.0.0.1:8443/
    
    # Skip SSL verification for self-signed backend
    SSLProxyEngine on
    SSLProxyVerify none
    SSLProxyCheckPeerCN off
    SSLProxyCheckPeerName off
</VirtualHost>
```

## Security Best Practices

### Certificate Security

1. **Strong Key Sizes**
   - RSA: Minimum 2048 bits (4096 for high security)
   - ECDSA: P-256 or P-384 curves

2. **Proper Permissions**
   ```bash
   chmod 644 /path/to/certificate.pem
   chmod 600 /path/to/private-key.pem
   chown root:root /path/to/certificates
   ```

3. **Regular Rotation**
   - Self-signed: Annual rotation
   - CA certificates: Follow CA policy
   - Emergency rotation: When compromise suspected

### TLS Configuration

1. **Disable Weak Protocols**
   - No SSLv3, TLSv1.0, TLSv1.1
   - Minimum TLS 1.2 (prefer TLS 1.3)

2. **Strong Cipher Suites**
   - ECDHE for Perfect Forward Secrecy
   - AES-GCM or ChaCha20-Poly1305 for encryption
   - SHA-256 or SHA-384 for hashing

3. **HSTS Headers** (if using reverse proxy)
   ```nginx
   add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
   ```

### Monitoring Security

1. **Certificate Expiration Monitoring**
   ```bash
   # Check certificate expiration
   openssl x509 -in /path/to/cert.pem -checkend $((30*24*3600))
   ```

2. **TLS Configuration Testing**
   ```bash
   # Use SSL Labs equivalent tools
   testssl.sh https://localhost:8443
   ```

3. **Access Logging**
   - Monitor failed TLS handshakes
   - Track certificate validation errors
   - Log suspicious connection patterns

## Troubleshooting

### Common Certificate Issues

#### 1. Certificate Not Found
```bash
# Error: certificate file not found
# Solution: Verify file paths
ls -la /path/to/cert.pem /path/to/key.pem

# Check configuration
grep -A 10 "tls:" ./agent-config.yaml
```

#### 2. Permission Denied
```bash
# Error: permission denied reading certificate
# Solution: Fix permissions
sudo chown senhub-agent:senhub-agent /path/to/certificates
chmod 644 /path/to/cert.pem
chmod 600 /path/to/key.pem
```

#### 3. Certificate/Key Mismatch
```bash
# Verify certificate and key match
cert_modulus=$(openssl x509 -noout -modulus -in cert.pem | openssl md5)
key_modulus=$(openssl rsa -noout -modulus -in key.pem | openssl md5)
echo "Cert: $cert_modulus"
echo "Key:  $key_modulus"
# Should be identical
```

#### 4. Hostname Mismatch
```bash
# Check certificate SAN
openssl x509 -in cert.pem -text -noout | grep -A1 "Subject Alternative Name"

# Add missing hostnames
./agent install --enable-https \
  --https-hosts "missing-hostname.com,another-host.local"
```

### TLS Connection Issues

#### 1. TLS Handshake Failures
```bash
# Test TLS connection
openssl s_client -connect localhost:8443 -debug

# Check supported TLS versions
nmap --script ssl-enum-ciphers -p 8443 localhost
```

#### 2. Cipher Suite Problems
```bash
# Test specific cipher
openssl s_client -connect localhost:8443 -cipher 'ECDHE+AESGCM'

# List available ciphers
openssl ciphers -v 'ALL:!aNULL:!eNULL'
```

#### 3. Certificate Chain Issues
```bash
# Verify certificate chain
openssl s_client -connect localhost:8443 -showcerts

# Check intermediate certificates
cat intermediate.pem >> cert.pem
```

### Debug Commands

#### Certificate Information
```bash
# View certificate details
openssl x509 -in cert.pem -text -noout

# Check certificate chain
openssl crl2pkcs7 -nocrl -certfile chain.pem | openssl pkcs7 -print_certs -noout

# Verify certificate against CA
openssl verify -CAfile ca.pem cert.pem
```

#### TLS Testing
```bash
# Test TLS configuration
curl -vI https://localhost:8443/health

# Test with specific TLS version
curl --tlsv1.3 -vI https://localhost:8443/health

# Test certificate validation
curl --cacert cert.pem https://localhost:8443/health
```

#### Agent Debugging
```bash
# Enable TLS debugging
./agent run --enable-https --verbose --debug-modules strategy.http

# Check certificate loading
./agent run --enable-https --debug-modules configuration
```

## Integration Examples

### Monitoring Tools with HTTPS

#### PRTG with Self-Signed Certificate
```xml
<!-- PRTG HTTP Advanced Sensor -->
<settings>
    <url>https://agent-host:8443/api/{agentkey}/prtg/metrics/cpu</url>
    <httpmethod>GET</httpmethod>
    <sslverification>false</sslverification>  <!-- For self-signed certs -->
    <timeout>30</timeout>
</settings>
```

#### Nagios with Certificate Validation
```bash
# Custom check command for HTTPS
define command {
    command_name    check_senhub_https
    command_line    /usr/lib/nagios/plugins/check_http \
                    -H $HOSTADDRESS$ -p 8443 -S \
                    -u "/api/{agentkey}/nagios/check/cpu_usage" \
                    -C 30  # Certificate expiration warning
}
```

#### Grafana with Custom CA
```yaml
# Grafana datasource configuration
datasources:
  - name: SenHub Agent
    type: prometheus
    url: https://agent-host:8443/api/{agentkey}/prometheus/metrics
    access: proxy
    tls_config:
      ca_file: /etc/ssl/certs/senhub-ca.pem
      cert_file: /etc/ssl/certs/grafana-client.pem
      key_file: /etc/ssl/private/grafana-client.key
```

---

**Last Updated**: January 2025  
**Version**: SenHub Agent v0.8.0+  
**Security Level**: Production Ready