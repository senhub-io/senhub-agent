package wildfly

import (
	"crypto/md5" // #nosec G501 - RFC 2617 digest authentication mandates MD5
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// WildFly's management interface authenticates with HTTP Digest
// (ManagementRealm) out of the box and answers 401 to Basic. The probe
// keeps the last challenge and signs each request with it, counting
// nonces, so a collection costs one request per call rather than two.
type digestAuth struct {
	mu        sync.Mutex
	challenge map[string]string
	nc        int
}

// parseDigestChallenge reads a WWW-Authenticate header. ok is false when
// it is not a Digest challenge.
func parseDigestChallenge(header string) (map[string]string, bool) {
	h := strings.TrimSpace(header)
	if len(h) < 7 || !strings.EqualFold(h[:6], "Digest") {
		return nil, false
	}
	out := map[string]string{}
	for _, part := range splitParams(h[6:]) {
		k, v, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return out, out["nonce"] != ""
}

// splitParams splits on commas outside quotes.
func splitParams(s string) []string {
	var parts []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case r == ',' && !quoted:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}

func (d *digestAuth) set(challenge map[string]string) {
	d.mu.Lock()
	d.challenge, d.nc = challenge, 0
	d.mu.Unlock()
}

// header returns the Authorization value for one request, or "" when no
// challenge has been seen yet.
func (d *digestAuth) header(method, uri, user, pass string) string {
	d.mu.Lock()
	c := d.challenge
	if c == nil {
		d.mu.Unlock()
		return ""
	}
	d.nc++
	nc := fmt.Sprintf("%08x", d.nc)
	d.mu.Unlock()

	sum := func(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) } // #nosec G401
	ha1 := sum(user + ":" + c["realm"] + ":" + pass)
	ha2 := sum(method + ":" + uri)
	cnonce := rand.Text()

	qop := ""
	for _, q := range strings.Split(c["qop"], ",") {
		if strings.TrimSpace(q) == "auth" {
			qop = "auth"
		}
	}
	var response string
	if qop == "auth" {
		response = sum(strings.Join([]string{ha1, c["nonce"], nc, cnonce, qop, ha2}, ":"))
	} else {
		response = sum(ha1 + ":" + c["nonce"] + ":" + ha2)
	}
	parts := []string{
		fmt.Sprintf(`username="%s"`, user),
		fmt.Sprintf(`realm="%s"`, c["realm"]),
		fmt.Sprintf(`nonce="%s"`, c["nonce"]),
		fmt.Sprintf(`uri="%s"`, uri),
		fmt.Sprintf(`response="%s"`, response),
		`algorithm=MD5`,
	}
	if qop != "" {
		parts = append(parts, "qop="+qop, "nc="+nc, fmt.Sprintf(`cnonce="%s"`, cnonce))
	}
	if o := c["opaque"]; o != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, o))
	}
	return "Digest " + strings.Join(parts, ", ")
}
