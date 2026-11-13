package http

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func sessionRouter(t *testing.T) *mux.Router {
	t.Helper()
	return surfaceRouter(t, map[string]interface{}{
		"endpoints": []interface{}{"prtg", "web"},
		"admin_key": "admin-key",
	})
}

func serve(router *mux.Router, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func TestBearerHeaderOpensTheAdministrationSurface(t *testing.T) {
	router := sessionRouter(t)
	req := httptest.NewRequest("GET", "/api/_/debug/logs", nil)
	req.Header.Set("Authorization", "Bearer admin-key")
	if code := serve(router, req).Code; code != http.StatusOK {
		t.Fatalf("bearer admin key answered %d, want 200", code)
	}
}

func TestBearerHeaderWithWrongOrReadKeyIsRefused(t *testing.T) {
	router := sessionRouter(t)
	for _, tok := range []string{"nope", "read-key", ""} {
		req := httptest.NewRequest("GET", "/api/_/debug/logs", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if code := serve(router, req).Code; code != http.StatusUnauthorized {
			t.Errorf("bearer %q answered %d, want 401", tok, code)
		}
	}
}

func TestURLFormStillWorksForTheAPI(t *testing.T) {
	router := sessionRouter(t)
	if code := serve(router, httptest.NewRequest("GET", "/api/admin-key/debug/logs", nil)).Code; code != http.StatusOK {
		t.Fatalf("URL form answered %d, want 200", code)
	}
	if code := serve(router, httptest.NewRequest("GET", "/api/wrong/debug/logs", nil)).Code; code != http.StatusUnauthorized {
		t.Fatalf("wrong URL key answered %d, want 401", code)
	}
}

func TestURLFormConsolePageSetsCookieAndRedirectsWithoutTheKey(t *testing.T) {
	router := sessionRouter(t)
	rec := serve(router, httptest.NewRequest("GET", "/web/admin-key/probes?new=1", nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/web/_/probes?new=1" {
		t.Fatalf("Location = %q", loc)
	}
	for name, vals := range rec.Header() {
		for _, v := range vals {
			if strings.Contains(v, "admin-key") {
				t.Errorf("header %s leaks the key: %s", name, v)
			}
		}
	}
	if strings.Contains(rec.Body.String(), "admin-key") {
		t.Errorf("body leaks the key: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

func TestCookieOpensConsolePageAndPageCarriesNoAdminKey(t *testing.T) {
	router := sessionRouter(t)
	c := sessionCookieOf(t, serve(router, httptest.NewRequest("GET", "/web/admin-key/overview", nil)))

	req := httptest.NewRequest("GET", "/web/_/overview", nil)
	req.AddCookie(c)
	rec := serve(router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie page answered %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "admin-key") {
		t.Error("page body carries the administration key")
	}
	if !strings.Contains(rec.Body.String(), "/web/_/") {
		t.Error("page links do not use the placeholder")
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}

	req = httptest.NewRequest("GET", "/api/_/info/system", nil)
	req.AddCookie(c)
	if code := serve(router, req).Code; code != http.StatusOK {
		t.Errorf("cookie API read answered %d", code)
	}
}

func TestForgedCookieIsRefused(t *testing.T) {
	router := sessionRouter(t)
	for _, v := range []string{"admin-key", "garbage", sessionToken("other")} {
		req := httptest.NewRequest("GET", "/api/_/debug/logs", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: v})
		if code := serve(router, req).Code; code != http.StatusUnauthorized {
			t.Errorf("cookie %q answered %d, want 401", v, code)
		}
	}
	if code := serve(router, httptest.NewRequest("GET", "/web/_/overview", nil)).Code; code != http.StatusUnauthorized {
		t.Errorf("no credential answered %d, want 401", code)
	}
}

func TestSessionCookieFlags(t *testing.T) {
	router := sessionRouter(t)

	c := sessionCookieOf(t, serve(router, httptest.NewRequest("GET", "/web/admin-key/overview", nil)))
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("flags wrong: %+v", c)
	}
	if c.Secure {
		t.Error("Secure set over plain HTTP")
	}

	req := httptest.NewRequest("GET", "/web/admin-key/overview", nil)
	req.TLS = &tls.ConnectionState{}
	c = sessionCookieOf(t, serve(router, req))
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("TLS flags wrong: %+v", c)
	}

	req = httptest.NewRequest("GET", "/web/admin-key/overview", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if c = sessionCookieOf(t, serve(router, req)); !c.Secure {
		t.Error("Secure not set behind a TLS-terminating proxy")
	}
}

func TestCookieOnlyStateChangeNeedsSameOrigin(t *testing.T) {
	router := sessionRouter(t)
	c := sessionCookieOf(t, serve(router, httptest.NewRequest("GET", "/web/admin-key/overview", nil)))

	cases := []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"no origin info", func(*http.Request) {}, http.StatusForbidden},
		{"cross-site fetch metadata", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, http.StatusForbidden},
		{"same-site fetch metadata", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, http.StatusForbidden},
		{"foreign Origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, http.StatusForbidden},
		{"null Origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, http.StatusForbidden},
		{"same-origin fetch metadata", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }, http.StatusOK},
		{"matching Origin", func(r *http.Request) { r.Header.Set("Origin", "http://agent.example:8080") }, http.StatusOK},
		{"cross-site metadata beats a matching Origin", func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", "cross-site")
			r.Header.Set("Origin", "http://agent.example:8080")
		}, http.StatusForbidden},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", "/api/_/admin/cache/clear", nil)
		req.Host = "agent.example:8080"
		req.AddCookie(c)
		tc.mutate(req)
		if got := serve(router, req).Code; got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestBearerStateChangeNeedsNoOriginCheck(t *testing.T) {
	router := sessionRouter(t)
	req := httptest.NewRequest("POST", "/api/_/admin/cache/clear", nil)
	req.Header.Set("Authorization", "Bearer admin-key")
	if code := serve(router, req).Code; code != http.StatusOK {
		t.Fatalf("bearer POST answered %d, want 200", code)
	}
}

func TestRotatedAdminKeyInvalidatesTheCookie(t *testing.T) {
	router := sessionRouter(t)
	req := httptest.NewRequest("GET", "/api/_/debug/logs", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken("admin-key-old")})
	if code := serve(router, req).Code; code != http.StatusUnauthorized {
		t.Fatalf("cookie from another key answered %d, want 401", code)
	}
}

func TestSensorURLsPageBuildsPollerURLsFromTheAgentKey(t *testing.T) {
	router := sessionRouter(t)
	c := sessionCookieOf(t, serve(router, httptest.NewRequest("GET", "/web/admin-key/outputs/http", nil)))
	req := httptest.NewRequest("GET", "/web/_/outputs/http", nil)
	req.AddCookie(c)
	rec := serve(router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "window.AGENT_READ_KEY = 'read-key'") {
		t.Error("the page does not hand the agent key to the sensor URL builder")
	}
}
