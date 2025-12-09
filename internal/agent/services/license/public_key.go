package license

// SenHubPublicKey is the RSA public key used to verify license tokens
// This key is embedded in the binary and corresponds to the private key
// used by SenHub platform to sign licenses
//
// ⚠️ PRODUCTION KEY: This is the RSA-4096 public key for production license verification.
// Generated on 2025-12-09
//
// SECURITY: The corresponding private key (.keys/production/senhub_private.pem) MUST be kept
// SECRET and stored securely in Sensor Factory. Only Sensor Factory can sign licenses.
// NEVER commit the private key to version control.
//
// Key Generation:
//
//	openssl genrsa -out .keys/production/senhub_private.pem 4096
//	openssl rsa -in .keys/production/senhub_private.pem -pubout -out .keys/production/senhub_public.pem
//	# Copy the public key from .keys/production/senhub_public.pem to this constant
const SenHubPublicKey = `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAyQWUGqhlWS5MeJMp3pIy
DyiZlZisNTneLufQy2XHYqQ29c8gKtdf3TltYXPM4LwtNidH4K16TVaAP6eXVAfT
r4YK8uKoczLClZdW7Eehu0iGXCg0gJb6+rYlCaeRuLl0uGtXDw0D5B74ZZuuzChy
IJSk2YSHi3mhtZlbsSIHM5HVC6Ymh/ptcxyKsuozBrzf4nrgdnEfhI5eL3tZ5ga8
NdUGx9L4KUnHOc2tx6gsajhPnOWi+Z5UY/3ck1l4w5tqVSWM7Gh30Nbx3YWZTcyi
mpmY2B+Ue1UwMwzDh6/rKoXII8UW2dE8Z7uvQAdDzqPtL7Tk9tWG/s7gIef7n6KK
RFCgaE+OCdeTd438WJHjfdqM2b0MVLCQModV5Ewno39+8hpnAKaeZaM/1Iy4nJYE
hwNY5XNsCgjrBqNmXRA2O5wpPZZeQ3SO41UdHCFi5HOrE8WS2TUjkJ7Gd3NMcMza
YEo0Y1OzHYjfRevWAGy76tn4YhKdUG7xDyZPx3/zE+XU7bBUn0g5NhvhPlXjoxjh
xBplZV0Qs0pgaoHDnoR3QSuYwoD29jU/3Lh5t7/AsLxJzp2xGqW2cth7kuf1mvoP
xpKWcsyzFCvMC+Pz/tS1C0aOtl4yZl7/k7wKX6mYKRPgM2LbXuNxA7gonECLDl5j
ojgktHLdogXsO8cHYmo4/+MCAwEAAQ==
-----END PUBLIC KEY-----`

// GetDefaultValidator returns a validator configured with the embedded SenHub public key
func GetDefaultValidator(gracePeriodDays int) (*JWTValidator, error) {
	return NewJWTValidator(SenHubPublicKey, gracePeriodDays)
}
