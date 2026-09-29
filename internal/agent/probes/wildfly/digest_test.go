package wildfly

import (
	"crypto/md5" // #nosec G501 - the test verifies an RFC 2617 MD5 response
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// digestGate answers like WildFly's ManagementRealm: Basic is refused,
// only a correctly signed Digest request reaches the handler.
type digestGate struct {
	next       http.Handler
	user, pass string
	challenges atomic.Int32
}

func (g *digestGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const realm, nonce = "ManagementRealm", "bm9uY2U="
	params, ok := parseDigestChallenge(r.Header.Get("Authorization"))
	if !ok || params["username"] != g.user || params["nonce"] != nonce {
		g.challenges.Add(1)
		w.Header().Set("WWW-Authenticate", `Digest realm="`+realm+`", nonce="`+nonce+`", opaque="00000000000000000000000000000000", algorithm=MD5, qop=auth`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	sum := func(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) } // #nosec G401
	ha1 := sum(g.user + ":" + realm + ":" + g.pass)
	ha2 := sum(r.Method + ":" + params["uri"])
	want := sum(strings.Join([]string{ha1, nonce, params["nc"], params["cnonce"], params["qop"], ha2}, ":"))
	if params["response"] != want {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	g.next.ServeHTTP(w, r)
}

func TestCollect_DigestAuthentication(t *testing.T) {
	gate := &digestGate{next: &wildflyHandler{}, user: "admin", pass: "admin"}
	srv := httptest.NewServer(gate)
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)
	if got["senhub.wildfly.up"] != 1 {
		t.Fatalf("up = %v, want 1 behind Digest authentication", got["senhub.wildfly.up"])
	}
	if _, ok := got["jvm.memory.heap.used"]; !ok {
		t.Fatal("no heap metric collected behind Digest authentication")
	}
	first := gate.challenges.Load()
	collectByName(t, p)
	if again := gate.challenges.Load(); again != first {
		t.Errorf("second collection was challenged %d more times; the challenge should be reused", again-first)
	}
}

func TestCollect_DigestWrongPassword(t *testing.T) {
	gate := &digestGate{next: &wildflyHandler{}, user: "admin", pass: "other"}
	srv := httptest.NewServer(gate)
	defer srv.Close()

	p := newTestProbe(t, srv)
	got := collectByName(t, p)
	if got["senhub.wildfly.up"] != 0 {
		t.Fatalf("up = %v, want 0 with a wrong password", got["senhub.wildfly.up"])
	}
}
