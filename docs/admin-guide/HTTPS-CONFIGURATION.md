# HTTPS/TLS Configuration Guide

## Overview

The SenHub Agent supports comprehensive HTTPS/TLS configuration for secure monitoring. This guide covers all aspects of TLS setup, from auto-generated certificates to production-ready configurations.

## TLS Modes

### 1. Disabled (Default HTTP)

**Use Case**: Local development, localhost-only access
**Security**: Basic HTTP, no encryption
**Configuration**: No TLS parameters needed

```bash
senhub-agent install
# Access: http://localhost:8080/web/{agentkey}/dashboard
```

### 2. Auto-Generated Certificates

**Use Case**: Quick HTTPS setup, internal networks, testing
**Security**: TLS encryption with self-signed certificates
**Configuration**: Automatic certificate generation

```bash
senhub-agent install --enable-https
# Access: https://localhost:8443/web/{agentkey}/dashboard
```

### 3. Provided Certificates

**Use Case**: Production environments, valid CA certificates
**Security**: Full TLS with trusted certificates
**Configuration**: User-provided certificate files

```bash
senhub-agent install --enable-https \
  --cert-file /path/to/cert.pem \
  --key-file /path/to/key.pem
# Access: https://your-domain.com:8443/web/{agentkey}/dashboard
```

> **Today these two flags are recorded and dropped** (#867). Set `cert_file`
> and `key_file` in the configuration after the install — see
> [Provided Certificates](#provided-certificates) — or the agent serves the
> self-signed pair it generated.

## Certificate Generation

### Automatic Generation Process

When using `--enable-https` without custom certificates:

1. **RSA Key Generation**: 2048-bit RSA private key
2. **Certificate Creation**: X.509 certificate with proper extensions
3. **SAN Configuration**: Subject Alternative Names from `--https-hosts`
4. **File Storage**: a `certs/` directory **next to the configuration file** —
   `/etc/senhub-agent/certs/` on a standard Linux install. It is not the
   working directory: a hardened unit runs with `ProtectHome=true`, and a
   certificate written under a home directory would be unreadable by the
   service user.
5. **Permission Setting**: `agent-cert.pem` and `agent-key.pem` are both `0600`

Certificates are generated **once**, when the installer creates a fresh
configuration. They are not re-generated on later starts, and an existing
`certs/` directory is left alone.

### Certificate Properties

```
Subject: CN=localhost, O=SenHub Agent
Issuer: Self-signed
Validity: 365 days from generation
Key Usage: Digital Signature, Key Encipherment
Extended Key Usage: Server Authentication
Subject Alternative Names: the values of --https-hosts (default: DNS:localhost, IP:127.0.0.1)
```

### Custom Subject Alternative Names

```bash
# Multiple hostnames for certificate
senhub-agent install --enable-https \
  --https-hosts "agent.company.com,192.168.1.100,monitoring.local,10.0.0.50"
```

This generates a certificate valid for exactly:
- `agent.company.com`
- `192.168.1.100`
- `monitoring.local`
- `10.0.0.50`

`--https-hosts` **replaces** the list, it does not extend it. `localhost` and
`127.0.0.1` are the default only when the flag is omitted — list them yourself
if you still want to reach the console over the loopback name:

```bash
senhub-agent install --enable-https \
  --https-hosts "localhost,127.0.0.1,agent.company.com,192.168.1.100"
```

## Configuration File TLS Section

The agent reads exactly four keys under `tls`: `enabled`, `min_tls_version`,
`cert_file` and `key_file`. There is no `mode` and no `auto_cert` block —
"auto-generated" and "provided" differ only in where the two files came from.

### Auto-Generated Certificates

What `install --enable-https` writes (paths are absolute, so the daemon's
working directory does not matter):

```yaml
storage:
  - name: http
    params:
      port: 8443
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        min_tls_version: "1.2"
        cert_file: "/etc/senhub-agent/certs/agent-cert.pem"
        key_file: "/etc/senhub-agent/certs/agent-key.pem"
```

### Provided Certificates

Point the same two keys at your own files:

```yaml
storage:
  - name: http
    params:
      port: 8443
      bind_address: "0.0.0.0"
      endpoints: ["prtg", "web", "nagios"]
      tls:
        enabled: true
        min_tls_version: "1.2"
        cert_file: "/etc/ssl/certs/agent.pem"
        key_file: "/etc/ssl/private/agent.key"
```

> **`--cert-file` / `--key-file` are accepted by `install` but do not reach the
> generated configuration** (#867): the file it writes always points at the
> self-signed pair. Until that is fixed, set `cert_file` / `key_file` in the
> configuration yourself after the install, then restart the service.

In the multi-file layout the same block lives in `strategies.d/00-http.yaml`
under a single top-level `http:` key.

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
senhub-agent install --enable-https \
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
senhub-agent install --enable-https \
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
senhub-agent install --enable-https --min-tls-version 1.2
```

#### TLS 1.3 (Recommended)
- **Compatibility**: Modern systems only
- **Security**: Latest encryption, improved performance
- **Use Case**: High-security environments, modern infrastructure

```bash
senhub-agent install --enable-https --min-tls-version 1.3
```

### Cipher Suites

There is no `cipher_suites` key. The agent serves the suites Go selects for
the negotiated version, which is a deliberate and maintained set: for TLS
1.3 the three suites the standard defines, and for TLS 1.2 the forward-secret
AEAD suites, with the insecure ones already excluded.

Overriding that list is more often a downgrade than a hardening, which is
why the option does not exist. Where a policy names a suite list, terminate
TLS on a reverse proxy and let it enforce the policy, as described below.

## Certificate Management

### Certificate Validation

#### Check Certificate Details
```bash
# View certificate information
openssl x509 -in /etc/senhub-agent/certs/agent-cert.pem -text -noout

# Check certificate validity
openssl x509 -in /etc/senhub-agent/certs/agent-cert.pem -checkend 86400  # Check if expires in 24h

# Verify certificate chain
openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt /etc/senhub-agent/certs/agent-cert.pem
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

### Renewal

#### Self-Signed Certificates
There is no automatic renewal. The pair is generated once, at install, and is
valid for 365 days; the agent does not check its expiry at startup. Watch the
expiry yourself (see *Certificate Expiration Monitoring* below) and renew by
replacing the two files and restarting the service:

```bash
sudo senhub-agent stop
# write a new pair to /etc/senhub-agent/certs/agent-cert.pem and agent-key.pem
sudo chown senhub:senhub /etc/senhub-agent/certs/agent-*.pem
sudo chmod 600 /etc/senhub-agent/certs/agent-*.pem
sudo senhub-agent start
```

A full `uninstall` + `install --enable-https` also regenerates the pair, at
the cost of the rest of the configuration.

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
sudo chown senhub:senhub /path/to/certificates
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
senhub-agent install --enable-https \
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
senhub-agent run --enable-https --filter strategy.http

# Check certificate loading
senhub-agent run --enable-https --filter configuration
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

**Applies to**: SenHub Agent 0.5.5 and later