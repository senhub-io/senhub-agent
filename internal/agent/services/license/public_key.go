package license

// SenHubPublicKey is the RSA public key used to verify license tokens
// This key is embedded in the binary and corresponds to the private key
// used by SenHub platform to sign licenses
//
// ⚠️ DEVELOPMENT KEY: This is a TEST RSA-4096 public key for development/testing.
// For production deployment, replace this with your production public key.
//
// SECURITY: The corresponding private key (license-private-key.pem) must be kept
// SECRET and stored securely in Sensor Factory. Only Sensor Factory can sign licenses.
//
// Key Generation:
//
//	cd scripts/
//	go run sensor-factory-license-generator.go --generate-keys
//	# Copy the public key from senhub-license-public-key.pem to this constant
const SenHubPublicKey = `-----BEGIN PUBLIC KEY-----
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAqnYgBiqaumyZHR4Q6ofr
Qf2jL9/OHvXYwVzGiwBYn535VCgfNNjBcQCo55Qyu5pj/9qX5eRZ3U6Huh4kxroR
aYkERInPTCoVq4eEOBCG9XY7fQ+GiTKRC5G+t89e2x9Hjw3Okbur4ACpkHZ2DZ/q
IkXC9wGrlbyu2bQSCvfXA0Ra9ShRbi1MMfOSmabK47WibzDKWd96qRd9h1PD6di1
QNiUXgz0vD2hvKJXQWYqchEOKls5bUTP2K2R6O0124Ev4VldBwGRMdwW8DKv0yam
qzGCdzF712fJjpLRwCaMDIUKqPsZrNO7G2pWIdZNmtZRujoOOn8AbDUL5kq+1hKp
LOjndtIvwjzVfcOY3sBsQxiPJlnhXDmaqnlryaItZhOzk3+u/1dXqXwLQ3IS1JCQ
1UgkMrPd065aTHdES0hgLijhlLtGTRdJACvC5zoiuULv5CjRINUNqihYxxqPu3M/
iFJO5R6MFCelbgmVue9cehW6bQGCYo8Ac8M9iYQ57J5p9kjJRPuXGeGT9qMLoJ7+
fSIMtFFwgd3pl01inUO4M+DpMEoIpWVFukXegm4mSOqEH8bFKTFHmeFV3KxM9RGE
tAMeqyiZksIWTl78o1raiEqCNJHH0hYZAQtkM7kDuWo4OYSKfmlRMgwG3NW9zehN
A3FXJDQ0+jjrRZ8n9sNNSp0CAwEAAQ==
-----END PUBLIC KEY-----`

// GetDefaultValidator returns a validator configured with the embedded SenHub public key
func GetDefaultValidator(gracePeriodDays int) (*JWTValidator, error) {
	return NewJWTValidator(SenHubPublicKey, gracePeriodDays)
}
