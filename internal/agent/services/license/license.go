package license

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// LicenseTier represents the subscription tier
type LicenseTier string

const (
	TierFree       LicenseTier = "free"
	TierPro        LicenseTier = "pro"
	TierEnterprise LicenseTier = "enterprise"
)

// LicenseClaims represents the JWT claims for the license.
// Standard JWT fields (exp, iat, iss, sub) are handled by RegisteredClaims.
// We use jwt.WithoutClaimsValidation() at parse time so that expired tokens
// are not rejected by the library — expiry is managed manually to support
// the grace period feature.
type LicenseClaims struct {
	Tier             LicenseTier `json:"tier"`
	AuthorizedProbes []string    `json:"authorized_probes"`
	jwt.RegisteredClaims
}

// License represents a validated license
type License struct {
	Tier             LicenseTier
	AuthorizedProbes []string
	ExpiresAt        time.Time
	IssuedAt         time.Time
	Subject          string
	IsExpired        bool
	GracePeriodDays  int
}

// Validator validates license tokens
type Validator interface {
	// ValidateLicense validates a JWT license token
	ValidateLicense(token string) (*License, error)

	// IsProbeAuthorized checks if a probe is authorized by the license
	IsProbeAuthorized(license *License, probeName string) bool

	// IsInGracePeriod checks if an expired license is still in grace period
	IsInGracePeriod(license *License) bool
}

// JWTValidator implements the Validator interface using JWT with RSA signature
type JWTValidator struct {
	publicKey       *rsa.PublicKey
	gracePeriodDays int
}

// NewJWTValidator creates a new JWT validator with the provided RSA public key
func NewJWTValidator(publicKeyPEM string, gracePeriodDays int) (*JWTValidator, error) {
	// Parse PEM block
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Parse RSA public key
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}

	return &JWTValidator{
		publicKey:       rsaPubKey,
		gracePeriodDays: gracePeriodDays,
	}, nil
}

// ValidateLicense validates a JWT licence token. RS256 with the
// embedded SenHub public key; expiry is checked manually so the
// grace-period semantics in IsInGracePeriod stay authoritative.
//
// The compact-licence path that used to live here was removed when
// the repository went open-source — its HMAC secret could not
// survive a public source tree. JWT is now the only supported
// format.
func (v *JWTValidator) ValidateLicense(tokenString string) (*License, error) {
	// Parse and validate JWT token.
	// WithoutClaimsValidation: expiry is managed manually to support grace periods.
	token, err := jwt.ParseWithClaims(tokenString, &LicenseClaims{}, func(token *jwt.Token) (interface{}, error) {
		// SECURITY: Verify signing method is specifically RS256 (not just any RSA method)
		// This prevents algorithm confusion/downgrade attacks
		method, ok := token.Method.(*jwt.SigningMethodRSA)
		if !ok || method.Alg() != "RS256" {
			return nil, fmt.Errorf("unexpected signing method: %v, expected RS256", token.Header["alg"])
		}
		return v.publicKey, nil
	}, jwt.WithoutClaimsValidation())

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Extract claims
	claims, ok := token.Claims.(*LicenseClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Read standard fields from RegisteredClaims
	var expiresAt, issuedAt time.Time
	if claims.RegisteredClaims.ExpiresAt != nil {
		expiresAt = claims.RegisteredClaims.ExpiresAt.Time
	}
	if claims.RegisteredClaims.IssuedAt != nil {
		issuedAt = claims.RegisteredClaims.IssuedAt.Time
	}
	isExpired := time.Now().After(expiresAt)

	license := &License{
		Tier:             claims.Tier,
		AuthorizedProbes: claims.AuthorizedProbes,
		ExpiresAt:        expiresAt,
		IssuedAt:         issuedAt,
		Subject:          claims.RegisteredClaims.Subject,
		IsExpired:        isExpired,
		GracePeriodDays:  v.gracePeriodDays,
	}

	return license, nil
}

// IsProbeAuthorized checks if a probe is authorized by the license
func (v *JWTValidator) IsProbeAuthorized(license *License, probeName string) bool {
	// Check if probe is in free tier (always authorized)
	if isFreeTierProbe(probeName) {
		return true
	}

	// SECURITY: If license is nil, only free tier probes are allowed
	// Paid probes require a valid license token
	if license == nil {
		return false
	}

	// Check if expired and not in grace period
	if license.IsExpired && !v.IsInGracePeriod(license) {
		return false
	}

	// Check if probe is in authorized list
	for _, authorizedProbe := range license.AuthorizedProbes {
		if authorizedProbe == probeName || authorizedProbe == "*" {
			return true
		}
	}

	return false
}

// IsInGracePeriod checks if an expired license is still in grace period
func (v *JWTValidator) IsInGracePeriod(license *License) bool {
	if !license.IsExpired {
		return false
	}

	gracePeriodEnd := license.ExpiresAt.Add(time.Duration(v.gracePeriodDays) * 24 * time.Hour)
	return time.Now().Before(gracePeriodEnd)
}

// Free tier probes - always available without license.
// linux_logs, windows_eventlog and filetail join the free tier as
// host-level observability sources on the same footing as cpu/memory/
// network/logicaldisk: they read logs from the machine the agent runs on,
// not a remote system (the OS log rails + generic flat-file tailing).
//
// snmp_poll is the deliberate exception to "remote = paid": it is the
// open-core wedge meant to replace PRTG's free SNMP polling, so generic
// SNMP collection is free. Deep vendor-specific SNMP (device profiles,
// discovery, vendor MIBs) remains paid — see the tiering strategy.
//
// snmp_trap follows snmp_poll: receiving generic SNMP traps is part of
// the same free PRTG-replacement wedge (the push counterpart of polling).
//
// icmp_check is free for the same wedge reason: ping/uptime is PRTG's
// most-deployed sensor class — the migration story needs it at zero cost.
//
// http_check follows: HTTP/TLS-cert checks are top-3 PRTG sensors and
// the OTel Collector gives them away (httpcheck receiver).
//
// otlp_receiver is free as universal collection: the agent acting as an
// edge collector ingesting OTLP streams from other instrumented sources
// is the same open-core "bring everything in" wedge, not a paid vendor
// integration.
var freeTierProbes = map[string]bool{
	"apache":           true,
	"cpu":              true,
	"memory":           true,
	"logicaldisk":      true,
	"network":          true,
	"linux_logs":       true,
	"windows_eventlog": true,
	"filetail":         true,
	"dns_latency":      true,
	"http_check":       true,
	"icmp_check":       true,
	"snmp_poll":        true,
	"snmp_trap":        true,
	"tcp_dial":         true,
	"otlp_receiver":    true,
	// prometheus_scrape: pull-side twin of otlp_receiver — scraping
	// exporters and appliances is universal collection, not a vendor
	// integration.
	"prometheus_scrape": true,
	// exec: the custom-sensor long tail every PRTG estate ends in;
	// Telegraf (exec) and Nagios (plugins) both cover it for free.
	"exec": true,
	// syslog: completes the universal log-collection set alongside
	// filetail and windows_eventlog (#298); receiving a standard
	// protocol is collection, not a vendor integration.
	"syslog": true,
	// nginx: stub_status scraping is the equivalent of a free OTel
	// Collector nginx receiver — web-server health is host-local
	// observability on the same machine the agent monitors.
	"nginx": true,
	// haproxy: HTTP-stats endpoint polling; same open-core wedge as
	// snmp_poll and prometheus_scrape — collecting from a standard
	// protocol endpoint on a local or accessible host is universal
	// collection, not a paid vendor integration.
	"haproxy": true,
	// varnish: host-local Varnish Cache observability — runs varnishstat
	// on the local machine. Same free rationale as cpu/memory/logicaldisk.
	"varnish": true,
}

// isFreeTierProbe checks if a probe is in the free tier
func isFreeTierProbe(probeName string) bool {
	return freeTierProbes[probeName]
}

// GetFreeTierProbes returns the list of free tier probes
func GetFreeTierProbes() []string {
	probes := make([]string, 0, len(freeTierProbes))
	for probe := range freeTierProbes {
		probes = append(probes, probe)
	}
	return probes
}

// IsProbeAuthorizable returns true when the probe can be authorized
// by at least one supported licence mechanism — either the free
// tier (no licence needed) or the paid-probe catalogue (claimable
// by a JWT licence).
//
// This is the structural check enforced by the registry invariant
// test in internal/agent/probes/registry_invariant_test.go. It does
// NOT take a licence token; it answers the question "would any
// well-formed licence be able to grant this probe?".
func IsProbeAuthorizable(probeName string) bool {
	if isFreeTierProbe(probeName) {
		return true
	}
	return paidProbes[probeName]
}

// VerifyBinding returns true when the licence is bound to the given
// agent key. A JWT licence binds via its Subject claim — an empty
// Subject is treated as a wildcard (test fixtures, dev tokens) so
// that operators using unsigned-tier-only setups are not blocked.
//
// The compact-licence binding (a 4-byte agent-key hash embedded in
// the token) was retired together with the compact format.
func VerifyBinding(_ string, agentKey string, lic *License) bool {
	if lic == nil {
		return false
	}
	return lic.Subject == "" || lic.Subject == agentKey
}
