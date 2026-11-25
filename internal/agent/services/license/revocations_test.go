package license

import (
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// signList builds an RS256 revocation-list token. headerTyp nil means
// no typ header; kid empty means the historical root key.
func signList(t *testing.T, priv interface{}, kid string, headerTyp any, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if headerTyp != nil {
		token.Header["typ"] = headerTyp
	}
	if kid != "" {
		token.Header["kid"] = kid
	}
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing revocation list: %v", err)
	}
	return signed
}

func listClaims(ver float64) jwt.MapClaims {
	return jwt.MapClaims{
		"typ":     RevocationsClaimType,
		"ver":     ver,
		"iat":     1700000000,
		"revoked": []string{"jti-1", "jti-2", ""},
	}
}

func TestValidateRevocationList(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	pem := publicKeyToPEM(pub)
	extraPriv, extraPub := generateTestRSAKeys(t)
	extraPEM := publicKeyToPEM(extraPub)
	v := newHardeningValidator(t, pem, "rotated-2025", extraPEM)

	tests := []struct {
		name    string
		token   func() string
		wantErr error
	}{
		{
			name: "valid list is accepted with version iat and revoked read",
			token: func() string {
				return signList(t, priv, "", RevocationsHeaderType, listClaims(7))
			},
		},
		{
			name: "valid list with known kid uses rotated key",
			token: func() string {
				return signList(t, extraPriv, "rotated-2025", RevocationsHeaderType, listClaims(8))
			},
		},
		{
			name: "unknown kid is rejected",
			token: func() string {
				return signList(t, priv, "ghost", RevocationsHeaderType, listClaims(9))
			},
			wantErr: ErrUnknownKeyID,
		},
		{
			name: "signature from another key is rejected",
			token: func() string {
				otherPriv, _ := generateTestRSAKeys(t)
				return signList(t, otherPriv, "", RevocationsHeaderType, listClaims(10))
			},
			wantErr: errAny,
		},
		{
			name: "missing header typ is rejected",
			token: func() string {
				return signList(t, priv, "", nil, listClaims(11))
			},
			wantErr: ErrWrongTokenType,
		},
		{
			name: "wrong header typ is rejected",
			token: func() string {
				return signList(t, priv, "", "JWT", listClaims(12))
			},
			wantErr: ErrWrongTokenType,
		},
		{
			name: "missing claim typ is rejected",
			token: func() string {
				claims := listClaims(13)
				delete(claims, "typ")
				return signList(t, priv, "", RevocationsHeaderType, claims)
			},
			wantErr: ErrWrongTokenType,
		},
		{
			name: "tier claim present is rejected",
			token: func() string {
				claims := listClaims(14)
				claims["tier"] = "pro"
				return signList(t, priv, "", RevocationsHeaderType, claims)
			},
			wantErr: ErrWrongTokenType,
		},
		{
			name: "authorized_probes claim present is rejected",
			token: func() string {
				claims := listClaims(15)
				claims["authorized_probes"] = []string{"dns"}
				return signList(t, priv, "", RevocationsHeaderType, claims)
			},
			wantErr: ErrWrongTokenType,
		},
		{
			name: "licence token presented as list is rejected",
			token: func() string {
				return signList(t, priv, "", RevocationsHeaderType, baseClaims())
			},
			wantErr: ErrWrongTokenType,
		},
		{
			name: "zero version is rejected",
			token: func() string {
				return signList(t, priv, "", RevocationsHeaderType, listClaims(0))
			},
			wantErr: errAny,
		},
		{
			name: "missing iat is rejected",
			token: func() string {
				claims := listClaims(16)
				delete(claims, "iat")
				return signList(t, priv, "", RevocationsHeaderType, claims)
			},
			wantErr: errAny,
		},
		{
			name: "wrong issuer is rejected",
			token: func() string {
				claims := listClaims(17)
				claims["iss"] = "NotSenHub"
				return signList(t, priv, "", RevocationsHeaderType, claims)
			},
			wantErr: errAny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list, err := v.ValidateRevocationList(tt.token())
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(tt.wantErr, errAny) && !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if list.Version == 0 {
				t.Fatal("expected non-zero version")
			}
			if list.IssuedAt.IsZero() {
				t.Fatal("expected non-zero iat")
			}
		})
	}
}

func TestValidateRevocationListRejectsHMAC(t *testing.T) {
	_, pub := generateTestRSAKeys(t)
	v := newHardeningValidator(t, publicKeyToPEM(pub), "", "")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, listClaims(18))
	token.Header["typ"] = RevocationsHeaderType
	signed, err := token.SignedString([]byte("hmac-secret"))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if _, err := v.ValidateRevocationList(signed); err == nil {
		t.Fatal("expected HS256 token to be rejected")
	}
}

func TestValidateRevocationListReadsRevoked(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	v := newHardeningValidator(t, publicKeyToPEM(pub), "", "")
	list, err := v.ValidateRevocationList(signList(t, priv, "", RevocationsHeaderType, listClaims(7)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty strings are dropped at parse time.
	if len(list.Revoked) != 2 || list.Revoked[0] != "jti-1" || list.Revoked[1] != "jti-2" {
		t.Fatalf("unexpected revoked entries: %v", list.Revoked)
	}
	if list.Version != 7 {
		t.Fatalf("unexpected version: %d", list.Version)
	}
}

func TestValidateLicenseRejectsRevocationList(t *testing.T) {
	priv, pub := generateTestRSAKeys(t)
	v := newHardeningValidator(t, publicKeyToPEM(pub), "", "")

	withHeader := signList(t, priv, "", RevocationsHeaderType, listClaims(19))
	if _, err := v.ValidateLicense(withHeader); !errors.Is(err, ErrWrongTokenType) {
		t.Fatalf("expected ErrWrongTokenType with header typ, got %v", err)
	}

	claims := listClaims(20)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if _, err := v.ValidateLicense(signed); !errors.Is(err, ErrWrongTokenType) {
		t.Fatalf("expected ErrWrongTokenType without header typ, got %v", err)
	}
}

func TestRevocationListRevokes(t *testing.T) {
	list := &RevocationList{Version: 3, Revoked: []string{"jti-1", "jti-2"}}

	if !list.Revokes("jti-1") {
		t.Fatal("expected jti-1 to be revoked")
	}
	if list.Revokes("jti-3") {
		t.Fatal("expected jti-3 not to be revoked")
	}
	if list.Revokes("") {
		t.Fatal("expected empty jti not to be revoked")
	}
	var nilList *RevocationList
	if nilList.Revokes("jti-1") {
		t.Fatal("expected nil list to revoke nothing")
	}
}

func TestCheckNewer(t *testing.T) {
	held := &RevocationList{Version: 5}
	newer := &RevocationList{Version: 6}
	older := &RevocationList{Version: 4}
	same := &RevocationList{Version: 5}

	if err := CheckNewer(newer, held); err != nil {
		t.Fatalf("expected newer list accepted, got %v", err)
	}
	if err := CheckNewer(newer, nil); err != nil {
		t.Fatalf("expected nil held accepted, got %v", err)
	}
	if err := CheckNewer(same, held); !errors.Is(err, ErrStaleRevocations) {
		t.Fatalf("expected ErrStaleRevocations for equal version, got %v", err)
	}
	if err := CheckNewer(older, held); !errors.Is(err, ErrStaleRevocations) {
		t.Fatalf("expected ErrStaleRevocations for older version, got %v", err)
	}
}

func TestCheckNewer_NilNextIsStale(t *testing.T) {
	if err := CheckNewer(nil, &RevocationList{Version: 1}); !errors.Is(err, ErrStaleRevocations) {
		t.Fatalf("CheckNewer(nil, held) = %v, want ErrStaleRevocations", err)
	}
}
