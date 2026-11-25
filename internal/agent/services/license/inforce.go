package license

import (
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// State is the single authoritative status of the licence in force. It is
// computed once per change (Evaluate) and read everywhere else, so readers
// never revalidate on their own.
type State string

const (
	StateNone    State = "none"    // no token configured
	StateValid   State = "valid"   // validated and not expired
	StateGrace   State = "grace"   // expired but inside the grace period
	StateExpired State = "expired" // expired, outside the grace period
	StateRevoked State = "revoked" // jti present in the revocation list
	StateInvalid State = "invalid" // refused by the validator
)

// Refusal reasons, matching the agentstate license gauge labels.
const (
	ReasonValidationFailed = "validation_failed"
	ReasonBindingMismatch  = "binding_mismatch"
	ReasonExpiredNoGrace   = "expired_no_grace"
	ReasonRevoked          = "revoked"
)

// InForce is the outcome of evaluating the configured licence token against
// the validator, the agent key and the held revocation list.
type InForce struct {
	Token       string
	License     *License // non-nil only in StateValid or StateGrace
	Parsed      *License // the parsed licence, even revoked or expired; nil if invalid
	State       State
	Reason      string          // empty unless Expired/Revoked/Invalid
	Err         error           // validation error in StateInvalid
	Revocations *RevocationList // the list held when evaluated, or nil
}

// Tier returns the effective tier: free when no licence is in force.
func (f InForce) Tier() LicenseTier {
	if f.License == nil {
		return TierFree
	}
	return f.License.Tier
}

// ExpiresIn returns the time left on the licence; negative during the grace
// period. false when no licence is in force.
func (f InForce) ExpiresIn(now time.Time) (time.Duration, bool) {
	if f.License == nil {
		return 0, false
	}
	return f.License.ExpiresAt.Sub(now), true
}

// NextChange is when the state changes by time alone: the end of a valid
// licence, or the end of the grace period. Zero when no time can change it.
func (f InForce) NextChange() time.Time {
	if f.License == nil {
		return time.Time{}
	}
	if f.State == StateGrace {
		return f.License.ExpiresAt.Add(time.Duration(f.License.GracePeriodDays) * 24 * time.Hour)
	}
	return f.License.ExpiresAt
}

// Evaluate resolves the licence state from the token, the validator and the
// held revocation list. Revocation wins over the grace period: a revoked
// licence is never honoured, expired or not.
func Evaluate(v Validator, token, agentKey string, list *RevocationList) InForce {
	f := InForce{Token: token, Revocations: list}
	if token == "" {
		f.State = StateNone
		return f
	}
	if v == nil {
		f.State = StateInvalid
		f.Reason = ReasonValidationFailed
		f.Err = errors.New("license validator unavailable")
		return f
	}
	lic, err := v.ValidateLicense(token)
	if err != nil {
		f.State = StateInvalid
		f.Reason = ReasonValidationFailed
		f.Err = err
		return f
	}
	f.Parsed = lic
	if agentKey != "" && !VerifyBinding(token, agentKey, lic) {
		f.State = StateInvalid
		f.Reason = ReasonBindingMismatch
		return f
	}
	if list.Revokes(lic.ID) {
		f.State = StateRevoked
		f.Reason = ReasonRevoked
		return f
	}
	if lic.IsExpired && !v.IsInGracePeriod(lic) {
		f.State = StateExpired
		f.Reason = ReasonExpiredNoGrace
		return f
	}
	if lic.IsExpired {
		f.State = StateGrace
		f.License = lic
		return f
	}
	f.State = StateValid
	f.License = lic
	return f
}

// Resolver memoizes the last evaluation so the licence is validated once per
// change, not once per reader.
type Resolver struct {
	mu        sync.Mutex
	validator Validator
	cached    InForce
	resolved  bool
	token     string
	agentKey  string
	revVer    uint64
}

func NewResolver(v Validator) *Resolver {
	return &Resolver{validator: v}
}

// Resolve returns the memoized InForce, re-evaluating only when the token,
// the agent key or the revocation list version changed.
func (r *Resolver) Resolve(token, agentKey string, list *RevocationList) InForce {
	revVer := uint64(0)
	if list != nil {
		revVer = list.Version
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.cached.NextChange()
	if r.resolved && r.token == token && r.agentKey == agentKey && r.revVer == revVer &&
		(next.IsZero() || time.Now().Before(next)) {
		return r.cached
	}
	r.cached = Evaluate(r.validator, token, agentKey, list)
	r.token = token
	r.agentKey = agentKey
	r.revVer = revVer
	r.resolved = true
	return r.cached
}

// Package-level publication: the sensor publishes, every reader (extension
// identity, metrics) reads without revalidating.
var current = struct {
	mu sync.Mutex
	f  InForce
	ok bool
}{}

// Publish records the licence in force as the process-wide current value.
func Publish(f InForce) {
	current.mu.Lock()
	current.f = f
	current.ok = true
	current.mu.Unlock()
}

// Current returns the last published InForce; false before anything was
// published.
func Current() (InForce, bool) {
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.f, current.ok
}

// ResetCurrentForTest clears the published value. Test-only.
func ResetCurrentForTest() {
	current.mu.Lock()
	current.f = InForce{}
	current.ok = false
	current.mu.Unlock()
}

// RefusalReason maps a licence or revocation-list rejection to the short word
// sent to the server. Signature-class failures (unknown kid, malformed or
// unverifiable token, delegation problems) must be distinguishable from
// schema-class ones server-side.
func RefusalReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrWrongTokenType):
		return "wrong_type"
	case errors.Is(err, ErrInvalidTier):
		return "tier"
	case errors.Is(err, ErrStaleRevocations):
		return "stale"
	case errors.Is(err, ErrUnknownKeyID),
		errors.Is(err, jwt.ErrTokenSignatureInvalid),
		errors.Is(err, jwt.ErrTokenMalformed),
		errors.Is(err, jwt.ErrTokenUnverifiable),
		errors.Is(err, ErrDelegationMissing),
		errors.Is(err, ErrDelegationInvalid),
		errors.Is(err, ErrDelegationScope):
		return "signature"
	default:
		return "schema"
	}
}
