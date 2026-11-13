package http

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// SessionKeyPlaceholder stands in the key segment of a console or admin
// URL when the administration key travels out of band, in an
// Authorization header or in the console cookie. The URL then carries
// nothing secret, which keeps the key out of browser history, proxy
// logs and Referer headers.
const SessionKeyPlaceholder = "_"

const sessionCookieName = "senhub_console"

// sessionToken is the cookie value for an administration key. It is
// derived rather than the key itself, so a rotated key invalidates the
// cookies issued under the old one and the raw key is never stored by
// the browser.
func sessionToken(adminKey string) string {
	sum := sha256.Sum256([]byte("senhub-console-session\x00" + adminKey))
	return hex.EncodeToString(sum[:])
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameOrigin reports whether a browser request was issued by a page of
// this very origin. A cookie is attached to cross-site requests too, so
// a state-changing request that only a cookie vouches for must prove
// where it came from. Sec-Fetch-Site is authoritative where sent; Origin
// covers the clients that do not send it; a request with neither is not
// a browser page of ours and is refused.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// authorizeSession checks an out-of-band administration credential: a
// Bearer header, else the console cookie. It answers 200, 401 or, for a
// cookie-only state-changing request that fails the same-origin check,
// 403.
func (a *AuthenticationManager) authorizeSession(r *http.Request) int {
	if tok := bearerToken(r); tok != "" {
		if a.ValidateAdminKey(tok) {
			return http.StatusOK
		}
		a.logger.Warn().
			Str("provided_key_prefix", keyPrefixForLog(tok)).
			Str("path", r.URL.Path).
			Msg("Invalid administration key in Authorization header")
		return http.StatusUnauthorized
	}

	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		a.logger.Debug().Str("path", r.URL.Path).Msg("No administration credential on a session request")
		return http.StatusUnauthorized
	}
	current := a.currentAdminKey()
	if current == "" || !constantTimeEqual(c.Value, sessionToken(current)) {
		a.logger.Warn().Str("path", r.URL.Path).Msg("Invalid console session cookie")
		return http.StatusUnauthorized
	}
	if !isSafeMethod(r.Method) && !sameOrigin(r) {
		a.logger.Warn().
			Str("path", r.URL.Path).
			Str("method", r.Method).
			Msg("Cookie-authenticated state-changing request refused: not same-origin")
		return http.StatusForbidden
	}
	return http.StatusOK
}

// sessionCookie is the cookie that carries the console session. Secure
// follows the transport: set when the request came over TLS, directly or
// through a TLS-terminating proxy that says so.
func (a *AuthenticationManager) sessionCookie(r *http.Request, adminKey string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken(adminKey),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	}
}

// startConsoleSession turns a console page request that carries the
// administration key in its URL into a cookie session: it sets the
// cookie and redirects to the same page under the placeholder, so the
// address bar, history and any later Referer never hold the key.
func (a *AuthenticationManager) startConsoleSession(w http.ResponseWriter, r *http.Request, adminKey string) {
	http.SetCookie(w, a.sessionCookie(r, adminKey))
	target := url.URL{
		Path:     "/web/" + SessionKeyPlaceholder + strings.TrimPrefix(r.URL.Path, "/web/"+adminKey),
		RawQuery: r.URL.RawQuery,
	}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

// NoReferrer keeps the console's address from travelling in a Referer
// header to any page its links lead to.
func NoReferrer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/web/") {
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
		next.ServeHTTP(w, r)
	})
}
