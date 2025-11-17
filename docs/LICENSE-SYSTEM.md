# SenHub Agent - License System Documentation

## Overview

The SenHub Agent uses a **JWT-based license system with RSA signatures** to control access to paid probes. This document explains the complete architecture, security model, and workflows for both development and production.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        License System                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Sensor Factory (Production)          SenHub Agent              │
│  ┌─────────────────────┐             ┌──────────────────┐      │
│  │ Private Key (Vault) │             │ Public Key       │      │
│  │  (4096-bit RSA)     │             │ (Embedded)       │      │
│  └──────────┬──────────┘             └────────┬─────────┘      │
│             │                                  │                │
│             │ Sign JWT                         │ Verify JWT    │
│             ▼                                  ▼                │
│  ┌─────────────────────┐             ┌──────────────────┐      │
│  │  Generate License   │────────────▶│ Validate License │      │
│  │  (JWT Token)        │  Customer   │ (Agent Startup)  │      │
│  └─────────────────────┘  Activates  └──────────────────┘      │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## License Tiers

### Free Tier (No License Required)
- **cpu** - CPU utilization monitoring
- **memory** - Memory usage monitoring
- **logicaldisk** - Disk space and I/O monitoring
- **network** - Network interface statistics

### Pro Tier (License Required)
Specific probes authorized by license:
- **redfish** - BMC/iDRAC/iLO hardware monitoring
- **citrix** - Citrix Virtual Apps and Desktops monitoring
- **syslog** - Syslog event collection
- **otel** - OpenTelemetry metrics collection
- **event** - Windows Event Log collection
- **ping_gateway** - Gateway connectivity monitoring
- **ping_webapp** - Web application availability
- **load_webapp** - Web application performance
- **wifi_signal_strength** - WiFi signal quality

### Enterprise Tier (License Required)
- **All probes** (wildcard "*" authorization)

## Security Model

### RSA Signature Verification

1. **Sensor Factory** signs licenses with **private key** (4096-bit RSA)
2. **Agent** validates licenses with **public key** (embedded in binary)
3. **Tampering detection**: Any modification invalidates the JWT signature

### Key Management

**Private Key (Production)**:
- Generated ONCE in secure environment
- Stored in secure vault (HashiCorp Vault, AWS Secrets Manager, etc.)
- NEVER committed to version control
- NEVER stored on disk (except during initial generation)
- Accessible ONLY by Sensor Factory backend

**Public Key**:
- Embedded in agent binary (`internal/agent/services/license/public_key.go`)
- Distributed with every agent installation
- Used to verify JWT signatures

## JWT License Format

### Claims Structure

```json
{
  "tier": "pro|enterprise",
  "authorized_probes": ["redfish", "citrix"] or ["*"],
  "exp": 1794241033,  // Expiration timestamp (Unix epoch)
  "iat": 1762705033,  // Issued at timestamp (Unix epoch)
  "iss": "SenHub",    // Issuer
  "sub": "customer-id" // Subject (customer identifier)
}
```

### Example JWT Token

```
eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdXRob3JpemVkX3Byb2JlcyI6WyJyZWRmaXNoIiwiY2l0cml4Il0sImV4cCI6MTc5NDI0MTAzMywiaWF0IjoxNzYyNzA1MDMzLCJpc3MiOiJTZW5IdWIiLCJzdWIiOiJ0ZXN0LWN1c3RvbWVyLXBybyIsInRpZXIiOiJwcm8ifQ.Gr-i74OG2WmiMn8DTjf5SiUmhm-DmGcDGVs_4EDNWror5riEUYYLZZGTDume8ejJYaQaRfXDhcOQPYHMg5YL64af0EeNiq8UFTMZi09N9ohU2NHMHT6GNRx_60r7klTXuVaT752jQfTfZqDgjnlpMoQaeovXHYMLq92Bn_KSHaqiJMJa3Nm4Vm0BaP86HkQBMA6UENda8_ErRWoVj1-LlT_6oRr5S8-yG6uJFD9AGLAc4ncEijBDRheJ8b4H4iEnS390Gfgyng7dvxb3P8_F_NLIUeawsjYdnDJoDYuX-PyeyrPuDFTPFWc2xLx47j5SGEkEnc6gaR1nxdWfEqQ3lApaAcIBov322AH35PrBZQ4RXRgtJVLK18ZjuztmJWjC8zY7g0CYxvRA3nkSUfwcjiamUeg5gM9uaEk8mtlTSmTkA4MPrEi3Mk_4CgYfNr4LGLt918zFrgxyXAhzmOuycMyqsiyVVTS9jWMsIlNLH7DMyoZNqPp_EmVf3EqaZbtcKxUeC95tTIUYgcyD9neTUbCBc-EBYANQ-A-2phafvKIEgHR8Bhz5ZjYunsK0Wz4IUrWJu7Io1bxIQporUUmoX8Qj0x3ugxT4Qf2VarN5M7t5VU19NPp78K6YOGJJHXFEKXp95WtVg5wrsHEhihhdtAxNanq_X9UXhBPvQO6IxU4
```

## Production Workflow

### 1. Initial Setup (ONE-TIME)

**In Sensor Factory secure environment:**

```bash
# Generate production RSA key pair
cd /path/to/sensor-factory
go run scripts/sensor-factory-license-generator.go --generate-keys

# Output:
# ✅ senhub-license-private-key.pem (KEEP SECRET)
# ✅ senhub-license-public-key.pem (distribute with agent)
```

**Security checklist:**
- [ ] Store private key in secure vault immediately
- [ ] Update agent's `public_key.go` with public key content
- [ ] Delete private key from disk after securing in vault
- [ ] Verify vault access is restricted to Sensor Factory backend only

### 2. Generating Customer Licenses

**In Sensor Factory:**

```bash
# Pro license for specific customer
go run sensor-factory-license-generator.go --generate-license \
  --customer-id "noble-age" \
  --tier "pro" \
  --probes "redfish,citrix" \
  --validity-days 365

# Enterprise license with all probes
go run sensor-factory-license-generator.go --generate-license \
  --customer-id "enterprise-corp" \
  --tier "enterprise" \
  --probes "*" \
  --validity-days 730

# Output: JWT token to send to customer
```

### 3. Customer Activation

**Customer receives JWT token and activates:**

```bash
# Activate license via CLI
./agent license activate eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...

# Or via Web UI (if implemented)
# Navigate to: http://localhost:8080/web/{agentkey}/license
```

### 4. License Validation

**Agent validates license on startup:**

```go
// In sensor.go
jwtValidator, err := license.GetDefaultValidator(7) // 7-day grace period
validatedLicense, err := validator.ValidateLicense(licenseToken)

if !validator.IsProbeAuthorized(validatedLicense, probeType) {
    // Probe not authorized - skip startup
}
```

## Grace Period

**7-day grace period** after license expiration:
- Allows time for license renewal
- Probes continue to function during grace period
- Warning messages logged
- After grace period: probes disabled, falls back to free tier

## Configuration Storage

License is stored in agent configuration file:

```yaml
agent:
  key: "agent-key-here"
  mode: offline
  license: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...  # JWT token

probes:
  - name: Dell iDRAC
    type: redfish  # Requires license
    params:
      endpoint: "https://idrac.example.com"
      username: "root"
      password: "password"
```

## CLI Commands

### Activate License
```bash
./agent license activate <JWT_TOKEN>
```

Validates JWT signature, shows license details, and saves to config file.

### Show License
```bash
./agent license show
```

Displays current license information:
- Tier
- Authorized probes
- Expiration date
- Status (ACTIVE / EXPIRED / GRACE PERIOD)

### Remove License
```bash
./agent license remove
```

Removes license from config (falls back to free tier).

## Development Workflow

### Testing with Mock Licenses

For development and testing, use `scripts/generate-license-keys.go`:

```bash
# Generate test keys and example licenses
go run scripts/generate-license-keys.go

# Output:
# - license-private-key.pem (TEST ONLY - DO NOT USE IN PRODUCTION)
# - license-public-key.pem
# - example-license-pro.jwt
# - example-license-enterprise.jwt
# - example-license-grace-period.jwt
```

**⚠️ WARNING**: Test keys must be replaced with production keys before deployment!

### Testing License Validation

```bash
# Build agent
make build-darwin

# Test with example license
./dist/senhub-agent_darwin_arm64 license activate $(cat example-license-pro.jwt)
./dist/senhub-agent_darwin_arm64 license show
```

## Security Best Practices

### For Sensor Factory

1. **Key Generation**
   - Generate keys in isolated, secure environment
   - Use 4096-bit RSA for production
   - Never reuse keys across environments

2. **Key Storage**
   - Store private key in vault with encryption at rest
   - Implement access controls (only backend services)
   - Enable audit logging for key access
   - Rotate keys periodically (annual recommended)

3. **License Generation**
   - Validate customer identity before issuing
   - Log all license generation events
   - Include customer ID in JWT subject
   - Set appropriate expiration dates

4. **Distribution**
   - Send licenses via secure channels (encrypted email, portal download)
   - Never log or expose private key
   - Provide clear activation instructions

### For Agent

1. **Public Key**
   - Embed public key in binary (compile-time)
   - Never load from external files (prevents substitution)
   - Verify key format on initialization

2. **License Validation**
   - Validate on every agent startup
   - Check signature before trusting claims
   - Enforce grace period strictly
   - Log all validation failures

3. **Configuration**
   - Store license in config file only
   - Use appropriate file permissions (0600)
   - Don't expose license in logs or APIs

## Troubleshooting

### Invalid License Error

```
❌ Invalid license code: crypto/rsa: verification error
```

**Cause**: JWT signature doesn't match public key.

**Solutions**:
- Verify public key in `public_key.go` matches Sensor Factory's
- Ensure license was generated with matching private key
- Check for token truncation or modification

### Probe Not Authorized

```
🚫 Probe not authorized by license - skipping
```

**Cause**: Probe type not in license's `authorized_probes` list.

**Solutions**:
- Verify license tier includes the probe
- Check probe type matches exactly (case-sensitive)
- Use enterprise license with "*" for all probes

### Grace Period Ended

```
❌ License expired and grace period ended - only free tier probes available
```

**Cause**: License expired more than 7 days ago.

**Solutions**:
- Generate new license with extended expiration
- Activate new license via CLI or Web UI

## File Reference

### Agent Files

```
internal/agent/services/license/
├── license.go              # JWT validator and license logic
├── public_key.go           # Embedded RSA public key
├── mock_validator.go       # Mock validator for testing
└── license_test.go         # Unit tests

internal/agent/services/sensor/
└── sensor.go               # License validation on probe startup

cmd/agent/
└── license.go              # CLI commands (activate, show, remove)
```

### Sensor Factory Files

```
scripts/
├── sensor-factory-license-generator.go  # Production license generator
└── generate-license-keys.go             # Development key generator

docs/
└── LICENSE-SYSTEM.md                    # This document
```

## API Integration (Future)

### Sensor Factory REST API (Proposed)

```http
POST /api/v1/licenses
Content-Type: application/json
Authorization: Bearer <admin-token>

{
  "customer_id": "noble-age",
  "tier": "pro",
  "authorized_probes": ["redfish", "citrix"],
  "validity_days": 365
}

Response:
{
  "license_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-11-09T17:17:13Z",
  "customer_id": "noble-age",
  "tier": "pro"
}
```

### Agent Web UI (Proposed)

```
GET /web/{agentkey}/license
→ Show current license status

POST /web/{agentkey}/license/activate
Body: { "license_token": "eyJhbGci..." }
→ Activate new license
```

## Migration from Free to Paid

### Customer Journey

1. **Start with Free Tier**
   - Deploy agent without license
   - Use free probes (cpu, memory, logicaldisk, network)

2. **Purchase License**
   - Customer contacts sales
   - Sensor Factory generates license
   - Customer receives JWT token

3. **Activate License**
   ```bash
   ./agent license activate <JWT_TOKEN>
   ```

4. **Configure Paid Probes**
   - Add redfish/citrix/other probes to config
   - Agent validates and starts authorized probes

5. **License Renewal**
   - Before expiration, generate new license
   - Activate via same process
   - Zero downtime during renewal

## FAQ

**Q: Can one license be used on multiple agents?**
A: Technically yes (JWT doesn't prevent copying), but this violates license terms. Server-side enforcement can be added in future.

**Q: What happens if license expires?**
A: 7-day grace period, then fallback to free tier probes only.

**Q: Can licenses be revoked?**
A: Not currently. Revocation would require agent to check with Sensor Factory on startup (online mode).

**Q: How to upgrade from Pro to Enterprise?**
A: Activate new Enterprise license - it replaces the existing one.

**Q: What if private key is compromised?**
A: Generate new key pair, update all agents with new public key, reissue all customer licenses.

## Conclusion

The SenHub Agent license system provides:
- ✅ Secure JWT-based authorization
- ✅ RSA signature verification (tamper-proof)
- ✅ Flexible tier system (free, pro, enterprise)
- ✅ Grace period for renewals
- ✅ Simple CLI activation
- ✅ Embedded validation (no internet required)

This system balances security, usability, and offline operation requirements.
