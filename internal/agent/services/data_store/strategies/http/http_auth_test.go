package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

const testAgentKey = "test-agent-key-1234567890abcdef"

func newAuthMgrForTest() *AuthenticationManager {
	args := &cliArgs.ParsedArgs{Env: "test", Verbose: false}
	return NewAuthenticationManager(testAgentKey, nil, logger.NewModuleLogger(logger.NewLogger(args), "test.auth"))
}

// TestConstantTimeEqual_BasicAndEdgeCases pins the constant-time comparison
// to its expected behavior — both branches matter for security (length
// short-circuit is OK; per-byte short-circuit would NOT be OK).
func TestConstantTimeEqual_BasicAndEdgeCases(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "ab", false},      // length differs
		{"", "", true},
		{"", "x", false},
		{"x", "", false},
		// Subtle: same length, fully different — must return false (and we trust
		// the implementation accumulates XOR rather than short-circuiting on
		// the first mismatch byte; we can't test the timing directly here).
		{strings.Repeat("a", 64), strings.Repeat("b", 64), false},
	}
	for _, c := range cases {
		if got := constantTimeEqual(c.a, c.b); got != c.want {
			t.Errorf("constantTimeEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// runBearerOrQuery exercises the AuthenticateBearerOrQuery handler and
// returns the response code + the WWW-Authenticate header value (if any).
func runBearerOrQuery(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	mgr := newAuthMgrForTest()
	rec := httptest.NewRecorder()
	mgr.AuthenticateBearerOrQuery(rec, req)
	return rec.Code, rec.Header().Get("WWW-Authenticate")
}

func TestBearerAuth_ValidHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testAgentKey)
	code, _ := runBearerOrQuery(t, req)
	if code != http.StatusOK {
		t.Errorf("valid bearer: got %d, want 200", code)
	}
}

func TestBearerAuth_InvalidHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong-key-12345678")
	code, _ := runBearerOrQuery(t, req)
	if code != http.StatusUnauthorized {
		t.Errorf("invalid bearer: got %d, want 401", code)
	}
}

func TestBearerAuth_MalformedHeader_FallsThroughToQueryThenChallenges(t *testing.T) {
	// Header that doesn't start with "Bearer " is ignored — implementation
	// then tries the query parameter (none here) and finally returns 401
	// with WWW-Authenticate. We document this behavior with table cases:
	cases := []struct {
		name, headerValue string
	}{
		{"basic-scheme", "Basic dXNlcjpwYXNz"},
		{"lowercase-bearer", "bearer " + testAgentKey},
		{"no-space-after-bearer", "Bearer" + testAgentKey},
		{"empty-token-after-bearer", "Bearer "},
		{"bearer-with-trailing-space", "Bearer "}, // same as above
		{"bearer-empty-string", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if c.headerValue != "" {
				req.Header.Set("Authorization", c.headerValue)
			}
			code, www := runBearerOrQuery(t, req)
			if code != http.StatusUnauthorized {
				t.Errorf("malformed %q: got %d, want 401", c.headerValue, code)
			}
			// The challenge header is only set when NO usable credential was
			// presented (no header AND no query). For an invalid header that
			// successfully parsed (Bearer + wrong token), no challenge is sent.
			if c.headerValue == "" && !strings.Contains(www, "Bearer") {
				t.Errorf("expected WWW-Authenticate Bearer challenge, got %q", www)
			}
		})
	}
}

func TestQueryAuth_ValidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics?token="+testAgentKey, nil)
	code, _ := runBearerOrQuery(t, req)
	if code != http.StatusOK {
		t.Errorf("valid query token: got %d, want 200", code)
	}
}

func TestQueryAuth_InvalidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics?token=wrong", nil)
	code, _ := runBearerOrQuery(t, req)
	if code != http.StatusUnauthorized {
		t.Errorf("invalid query token: got %d, want 401", code)
	}
}

func TestNoAuth_Returns401WithChallenge(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	code, www := runBearerOrQuery(t, req)
	if code != http.StatusUnauthorized {
		t.Errorf("no auth: got %d, want 401", code)
	}
	if !strings.Contains(www, `Bearer realm="senhub-agent"`) {
		t.Errorf("WWW-Authenticate should advertise Bearer realm; got %q", www)
	}
}

// TestBearerWinsOverQuery — when both are present and the bearer is valid,
// the query is ignored. When the bearer is INVALID, the function rejects
// outright (does not fall back to the query) — documented behavior.
func TestBearerWinsOverQuery_ValidBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics?token=wrong", nil)
	req.Header.Set("Authorization", "Bearer "+testAgentKey)
	code, _ := runBearerOrQuery(t, req)
	if code != http.StatusOK {
		t.Errorf("valid bearer + wrong query: got %d, want 200", code)
	}
}

func TestBearerWinsOverQuery_InvalidBearerDoesNotFallback(t *testing.T) {
	// If a client presents a malformed-by-content bearer, we reject — we do
	// NOT fall back to the query token. This avoids confused-deputy issues
	// where a stale bad header masks a working token.
	req := httptest.NewRequest(http.MethodGet, "/metrics?token="+testAgentKey, nil)
	req.Header.Set("Authorization", "Bearer wrong-key-12345678")
	code, _ := runBearerOrQuery(t, req)
	if code != http.StatusUnauthorized {
		t.Errorf("invalid bearer + valid query: got %d, want 401 (no fallback)", code)
	}
}
