# SenHub Agent - Scripts Directory

This directory contains utility scripts for the SenHub Agent project.

## License System Scripts

### 🧪 Development: `generate-keys/generate-license-keys.go`

**Purpose**: Generate test RSA keys and example JWT licenses for development and testing.

**Usage**:
```bash
go run generate-keys/generate-license-keys.go
```

**Outputs**:
- `license-private-key.pem` - Test RSA private key (4096-bit)
- `license-public-key.pem` - Test RSA public key
- `example-license-pro.jwt` - Example Pro tier license
- `example-license-enterprise.jwt` - Example Enterprise tier license
- `example-license-grace-period.jwt` - Example expired license

**⚠️ WARNING**:
- These are TEST KEYS ONLY
- DO NOT use in production
- Private key should be deleted before deployment
- Public key should be replaced with production key

### 🏭 Production: `license-generator/sensor-factory-license-generator.go`

**Purpose**: Generate production RSA keys and customer licenses for Sensor Factory.

**Usage**:

1. **ONE-TIME: Generate production key pair**
   ```bash
   go run license-generator/sensor-factory-license-generator.go --generate-keys
   ```

   Outputs:
   - `senhub-license-private-key.pem` - Production private key (store in vault!)
   - `senhub-license-public-key.pem` - Production public key (embed in agent)

2. **ONGOING: Generate customer licenses**
   ```bash
   go run license-generator/sensor-factory-license-generator.go --generate-license \
     --customer-id "customer-name" \
     --tier "pro" \
     --probes "redfish,citrix" \
     --validity-days 365
   ```

**Features**:
- Interactive confirmation for key generation
- Detailed output with security reminders
- Customer-specific license generation
- Flexible probe authorization
- Configurable validity periods

**Security Requirements**:
- Private key MUST be stored in secure vault (HashiCorp Vault, AWS Secrets Manager, etc.)
- Private key should NEVER be committed to version control
- Public key should be embedded in agent binary
- Access to private key should be restricted to Sensor Factory backend only

## Key Differences

| Feature | Development Script | Production Script |
|---------|-------------------|-------------------|
| **Purpose** | Testing & examples | Production licenses |
| **Key names** | `license-*.pem` | `senhub-license-*.pem` |
| **Security** | Casual (test only) | Critical (vault required) |
| **CLI** | No arguments | Full CLI interface |
| **Examples** | Auto-generated | On-demand |
| **Validation** | None | Interactive confirmation |

## Integration with Sensor Factory

The production script (`sensor-factory-license-generator.go`) should be:

1. **Copied** to Sensor Factory repository
2. **Integrated** into Sensor Factory's license management system
3. **Protected** with appropriate access controls
4. **Monitored** with audit logging

Example integration:

```go
// In Sensor Factory backend
func GenerateLicense(customerID, tier string, probes []string, days int) (string, error) {
    // Load private key from vault
    privateKey := vault.GetSecret("senhub-license-private-key")

    // Generate JWT license
    claims := jwt.MapClaims{
        "tier":              tier,
        "authorized_probes": probes,
        "exp":               time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix(),
        "iat":               time.Now().Unix(),
        "iss":               "SenHub",
        "sub":               customerID,
    }

    token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
    return token.SignedString(privateKey)
}
```

## Documentation

For complete documentation on the license system, see:
- [`/docs/LICENSE-SYSTEM.md`](../docs/LICENSE-SYSTEM.md) - Complete license system architecture
- [`/internal/agent/services/license/`](../internal/agent/services/license/) - License validation code

## Other Scripts

(Add other scripts documentation here as needed)
