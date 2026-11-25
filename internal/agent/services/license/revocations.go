package license

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// RevocationsHeaderType is the JWT header typ of a signed revocation
// list. Distinct from the licence typ so a list can never be mistaken
// for a licence (and vice versa) even when both share the root key.
const RevocationsHeaderType = "senhub-revocations+jwt"

// RevocationsClaimType mirrors the header type as a claim, so a list
// signed without a header typ is still identifiable.
const RevocationsClaimType = "senhub-revocations"

// ErrWrongTokenType is returned when a token of one type (licence,
// revocation list) is presented where another is expected.
var ErrWrongTokenType = errors.New("token of the wrong type")

// ErrStaleRevocations is returned when a fetched revocation list is not
// strictly newer than the one already held; versions must be monotonic
// so an attacker cannot replay an old list to un-revoke a licence.
var ErrStaleRevocations = errors.New("revocation list is not newer than the one held")

// RevocationList is the decoded payload of a signed revocation list.
type RevocationList struct {
	Version  uint64
	IssuedAt time.Time
	Revoked  []string
}

// Revokes reports whether the given jti is revoked by this list.
func (l *RevocationList) Revokes(jti string) bool {
	if l == nil || jti == "" {
		return false
	}
	for _, revoked := range l.Revoked {
		if revoked == jti {
			return true
		}
	}
	return false
}

// NewerThan reports whether l is strictly newer than held.
func (l *RevocationList) NewerThan(held *RevocationList) bool {
	if l == nil {
		return false
	}
	if held == nil {
		return true
	}
	return l.Version > held.Version
}

// CheckNewer returns ErrStaleRevocations (wrapped with both versions)
// unless next is strictly newer than held.
func CheckNewer(next, held *RevocationList) error {
	if next.NewerThan(held) {
		return nil
	}
	if next == nil {
		return fmt.Errorf("%w: no list", ErrStaleRevocations)
	}
	return fmt.Errorf("%w: held version %d, next version %d", ErrStaleRevocations, held.Version, next.Version)
}

// revocationClaims is the parse target for a revocation list. Tier and
// AuthorizedProbes are licence-only claims: their presence means a
// licence payload was stamped with the revocation typ, so it must be
// refused rather than half-validated.
type revocationClaims struct {
	Typ              *string   `json:"typ"`
	Ver              uint64    `json:"ver"`
	Revoked          []string  `json:"revoked"`
	Tier             *string   `json:"tier"`
	AuthorizedProbes *[]string `json:"authorized_probes"`
	jwt.RegisteredClaims
}

// ValidateRevocationList verifies a signed revocation list: RS256 with
// the root key (historical or selected by kid), the revocation typ in
// both header and claims, a strictly positive version and a present
// iat. iss is optional but, when present, must be the licence issuer.
func (v *JWTValidator) ValidateRevocationList(token string) (*RevocationList, error) {
	claims := &revocationClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok || t.Method.Alg() != "RS256" {
			return nil, fmt.Errorf("unexpected signing method: %v, expected RS256", t.Header["alg"])
		}
		if typ, _ := t.Header["typ"].(string); typ != RevocationsHeaderType {
			return nil, fmt.Errorf("%w: header typ %q, want %q", ErrWrongTokenType, typ, RevocationsHeaderType)
		}
		kid, err := headerKID(t.Header)
		if err != nil {
			return nil, err
		}
		return v.selectRootKey(kid)
	}, jwt.WithoutClaimsValidation())
	if err != nil {
		return nil, fmt.Errorf("failed to parse revocation list: %w", err)
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("invalid revocation list token")
	}

	if claims.Typ == nil || *claims.Typ != RevocationsClaimType {
		presented := ""
		if claims.Typ != nil {
			presented = *claims.Typ
		}
		return nil, fmt.Errorf("%w: claim typ %q, want %q", ErrWrongTokenType, presented, RevocationsClaimType)
	}
	if claims.Tier != nil {
		return nil, fmt.Errorf("%w: revocation list carries a licence-only tier claim", ErrWrongTokenType)
	}
	if claims.AuthorizedProbes != nil {
		return nil, fmt.Errorf("%w: revocation list carries licence-only authorized_probes claim", ErrWrongTokenType)
	}
	if claims.Ver == 0 {
		return nil, fmt.Errorf("revocation list version must be strictly positive")
	}
	if claims.RegisteredClaims.IssuedAt == nil {
		return nil, fmt.Errorf("revocation list is missing iat")
	}
	if claims.RegisteredClaims.Issuer != "" && claims.RegisteredClaims.Issuer != LicenceIssuer {
		return nil, fmt.Errorf("unexpected issuer %q, want %q", claims.RegisteredClaims.Issuer, LicenceIssuer)
	}

	list := &RevocationList{
		Version:  claims.Ver,
		IssuedAt: claims.RegisteredClaims.IssuedAt.Time,
	}
	for _, jti := range claims.Revoked {
		if jti != "" {
			list.Revoked = append(list.Revoked, jti)
		}
	}
	return list, nil
}
