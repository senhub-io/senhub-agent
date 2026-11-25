package license

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testAgentKey = "0f14d0ab-9605-4a62-a9e4-5ed26688389b"

// expiredClaims is a licence that expired age ago; the validator under
// test grants 7 days of grace.
func expiredClaims(age time.Duration) jwt.MapClaims {
	c := baseClaims()
	c["jti"] = "j1"
	c["sub"] = testAgentKey
	c["exp"] = time.Now().Add(-age).Unix()
	return c
}

func TestEvaluate(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	pem := publicKeyToPEM(pub)
	v := newHardeningValidator(t, pem, "", "")

	revoked := &RevocationList{Version: 1, Revoked: []string{"j1", "j2"}}

	tests := []struct {
		name      string
		token     string
		agentKey  string
		list      *RevocationList
		wantState State
		wantReas  string
		wantTier  LicenseTier
	}{
		{
			name:      "noTokenIsNone",
			wantState: StateNone,
			wantTier:  TierFree,
		},
		{
			name:      "garbageTokenIsInvalidWithValidationFailed",
			token:     "not-a-jwt",
			wantState: StateInvalid,
			wantReas:  ReasonValidationFailed,
			wantTier:  TierFree,
		},
		{
			name: "boundToAnotherAgentKeyIsBindingMismatch",
			token: func() string {
				c := baseClaims()
				c["sub"] = "11111111-2222-3333-4444-555555555555"
				return createSignedJWT(t, priv, c)
			}(),
			agentKey:  testAgentKey,
			wantState: StateInvalid,
			wantReas:  ReasonBindingMismatch,
			wantTier:  TierFree,
		},
		{
			name:      "listedJtiIsRevokedEvenWithALicence",
			token:     createSignedJWT(t, priv, baseClaimsWithJTI()),
			agentKey:  testAgentKey,
			list:      revoked,
			wantState: StateRevoked,
			wantReas:  ReasonRevoked,
			wantTier:  TierFree,
		},
		{
			name:      "revokedExpiredLicenceGetsNoGrace",
			token:     createSignedJWT(t, priv, expiredClaims(48*time.Hour)),
			agentKey:  testAgentKey,
			list:      revoked,
			wantState: StateRevoked,
			wantReas:  ReasonRevoked,
			wantTier:  TierFree,
		},
		{
			name:      "expiredInsideGracePeriodIsGrace",
			token:     createSignedJWT(t, priv, expiredClaims(48*time.Hour)),
			agentKey:  testAgentKey,
			wantState: StateGrace,
			wantTier:  TierPro,
		},
		{
			name:      "expiredOutsideGracePeriodIsExpired",
			token:     createSignedJWT(t, priv, expiredClaims(10*24*time.Hour)),
			agentKey:  testAgentKey,
			wantState: StateExpired,
			wantReas:  ReasonExpiredNoGrace,
			wantTier:  TierFree,
		},
		{
			name:      "notExpiredIsValid",
			token:     createSignedJWT(t, priv, baseClaimsWithJTI()),
			list:      &RevocationList{Version: 1},
			wantState: StateValid,
			wantTier:  TierPro,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(v, tt.token, tt.agentKey, tt.list)
			if got.State != tt.wantState {
				t.Fatalf("state: got %q, want %q", got.State, tt.wantState)
			}
			if got.Reason != tt.wantReas {
				t.Errorf("reason: got %q, want %q", got.Reason, tt.wantReas)
			}
			if got.Tier() != tt.wantTier {
				t.Errorf("tier: got %q, want %q", got.Tier(), tt.wantTier)
			}
			if got.Token != tt.token {
				t.Errorf("token not echoed back")
			}
			if got.Revocations != tt.list {
				t.Errorf("revocations not echoed back")
			}
			wantLic := tt.wantState == StateValid || tt.wantState == StateGrace
			if (got.License != nil) != wantLic {
				t.Errorf("license: got %v, want non-nil %v", got.License != nil, wantLic)
			}
			wantParsed := wantLic || tt.wantState == StateRevoked || tt.wantState == StateExpired ||
				tt.wantState == StateInvalid && tt.wantReas == ReasonBindingMismatch
			if (got.Parsed != nil) != wantParsed {
				t.Errorf("parsed: got %v, want non-nil %v", got.Parsed != nil, wantParsed)
			}
		})
	}
}

func baseClaimsWithJTI() jwt.MapClaims {
	c := baseClaims()
	c["jti"] = "j2"
	c["sub"] = testAgentKey
	return c
}

func TestEvaluate_NilValidatorIsInvalid(t *testing.T) {
	got := Evaluate(nil, "sometoken", "", nil)
	if got.State != StateInvalid || got.Reason != ReasonValidationFailed || got.Err == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestExpiresIn(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		f        InForce
		wantOK   bool
		negative bool
	}{
		{name: "validIsPositive", f: InForce{License: &License{ExpiresAt: now.Add(time.Hour)}}, wantOK: true},
		{name: "graceIsNegative", f: InForce{License: &License{ExpiresAt: now.Add(-time.Hour)}}, wantOK: true, negative: true},
		{name: "noneIsFalse", f: InForce{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := tt.f.ExpiresIn(now)
			if ok != tt.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if tt.negative && d >= 0 {
				t.Errorf("duration: got %v, want negative", d)
			}
			if !tt.negative && d <= 0 {
				t.Errorf("duration: got %v, want positive", d)
			}
		})
	}
}

// countingValidator wraps the real validator to count ValidateLicense calls.
type countingValidator struct {
	*JWTValidator
	calls int
}

func (c *countingValidator) ValidateLicense(token string) (*License, error) {
	c.calls++
	return c.JWTValidator.ValidateLicense(token)
}

func TestResolverValidatesOncePerChange(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	v := &countingValidator{JWTValidator: newHardeningValidator(t, publicKeyToPEM(pub), "", "")}
	token := createSignedJWT(t, priv, baseClaimsWithJTI())
	list := &RevocationList{Version: 1}

	r := NewResolver(v)
	first := r.Resolve(token, testAgentKey, list)
	for i := 0; i < 2; i++ {
		again := r.Resolve(token, testAgentKey, list)
		if again.State != first.State {
			t.Fatalf("state changed on memoized resolve: %q vs %q", again.State, first.State)
		}
	}
	if v.calls != 1 {
		t.Fatalf("identical resolves: got %d validations, want 1", v.calls)
	}

	other := baseClaimsWithJTI()
	other["jti"] = "j3"
	r.Resolve(createSignedJWT(t, priv, other), testAgentKey, list)
	if v.calls != 2 {
		t.Fatalf("token change: got %d validations, want 2", v.calls)
	}

	r.Resolve(token, "11111111-2222-3333-4444-555555555555", list)
	if v.calls != 3 {
		t.Fatalf("agent key change: got %d validations, want 3", v.calls)
	}

	r.Resolve(token, testAgentKey, &RevocationList{Version: 2})
	if v.calls != 4 {
		t.Fatalf("revocation version change: got %d validations, want 4", v.calls)
	}

	// A nil list has version 0, distinct from any real list.
	r.Resolve(token, testAgentKey, nil)
	if v.calls != 5 {
		t.Fatalf("nil list change: got %d validations, want 5", v.calls)
	}
}

func TestPublishAndCurrent(t *testing.T) {
	ResetCurrentForTest()
	t.Cleanup(ResetCurrentForTest)

	if _, ok := Current(); ok {
		t.Fatal("Current returned true before any publication")
	}

	f := InForce{State: StateValid, License: &License{Tier: "pro"}}
	Publish(f)
	got, ok := Current()
	if !ok {
		t.Fatal("Current returned false after publication")
	}
	if got.State != StateValid || got.Tier() != "pro" {
		t.Fatalf("got %+v", got)
	}
}

func TestRefusalReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "wrongTokenType", err: ErrWrongTokenType, want: "wrong_type"},
		{name: "invalidTier", err: ErrInvalidTier, want: "tier"},
		{name: "staleRevocations", err: ErrStaleRevocations, want: "stale"},
		{name: "unknownKeyID", err: ErrUnknownKeyID, want: "signature"},
		{name: "signatureInvalid", err: jwt.ErrTokenSignatureInvalid, want: "signature"},
		{name: "wrappedSignatureInvalid", err: fmt.Errorf("validating: %w", jwt.ErrTokenSignatureInvalid), want: "signature"},
		{name: "malformed", err: jwt.ErrTokenMalformed, want: "signature"},
		{name: "unverifiable", err: fmt.Errorf("load keys: %w", jwt.ErrTokenUnverifiable), want: "signature"},
		{name: "delegationMissing", err: ErrDelegationMissing, want: "signature"},
		{name: "delegationInvalid", err: ErrDelegationInvalid, want: "signature"},
		{name: "delegationScope", err: ErrDelegationScope, want: "signature"},
		{name: "legacyCompactLicense", err: ErrLegacyCompactLicense, want: "schema"},
		{name: "other", err: errors.New("boom"), want: "schema"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RefusalReason(tt.err); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
