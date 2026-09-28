package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// surfaceRouter builds the output with the given parameters and returns
// its routes, the way the agent serves them.
func surfaceRouter(t *testing.T, params map[string]interface{}) *mux.Router {
	t.Helper()
	base := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	cfg := configuration.NewAgentConfiguration("read-key", "", base)
	s, ok := NewHTTPSyncStrategy(cfg, params, base).(*HTTPSyncStrategy)
	if !ok {
		t.Fatal("strategy cast")
	}
	return NewHTTPHandlers(s).SetupRoutes()
}

func surfaceStatus(t *testing.T, router *mux.Router, method, url string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, url, nil))
	return rec.Code
}

// The key a monitoring tool is given reads this agent. It must not be
// able to change it. Before the two were told apart, the key handed to
// PRTG also cleared the metric cache, injected values into it and
// changed the agent's log levels — and that key travels in the URL
// path, so it lands in every access log between the two.
func TestTheReadKeyDoesNotOpenTheAdministrationSurface(t *testing.T) {
	router := surfaceRouter(t, map[string]interface{}{
		"endpoints": []interface{}{"prtg", "web"},
		"admin_key": "admin-key",
	})

	for _, route := range []struct{ method, path string }{
		{"POST", "/api/read-key/admin/cache/clear"},
		{"POST", "/api/read-key/debug/inject-real-metrics"},
		{"POST", "/api/read-key/debug/logs"},
		{"GET", "/api/read-key/debug/logs"},
		{"POST", "/api/read-key/config/test"},
		{"GET", "/api/read-key/config/probes"},
		{"GET", "/web/read-key/dashboard"},
	} {
		if code := surfaceStatus(t, router, route.method, route.path); code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d to the read key, want 401", route.method, route.path, code)
		}
	}
}

// The administration key carries the read privilege: the console reads
// as much as it writes, and making an operator juggle two keys in one
// page would only invite them to share the stronger one.
func TestTheAdministrationKeyOpensBothSurfaces(t *testing.T) {
	router := surfaceRouter(t, map[string]interface{}{
		"endpoints": []interface{}{"prtg", "web"},
		"admin_key": "admin-key",
	})

	for _, route := range []struct{ method, path string }{
		{"GET", "/api/admin-key/debug/logs"},
		{"GET", "/api/admin-key/config/probes"},
		{"GET", "/web/admin-key/dashboard"},
		{"GET", "/api/admin-key/info/system"},
		{"GET", "/api/admin-key/prtg/probes"},
	} {
		if code := surfaceStatus(t, router, route.method, route.path); code == http.StatusUnauthorized {
			t.Errorf("%s %s refused the administration key", route.method, route.path)
		}
	}
}

// An installation that feeds PRTG never needed the administration
// surface and carried it anyway. Without a key it is not served at all:
// a surface nobody can authenticate to is better absent than answering
// Unauthorized to the world.
func TestWithoutAnAdministrationKeyTheSurfaceIsNotServed(t *testing.T) {
	router := surfaceRouter(t, map[string]interface{}{
		"endpoints": []interface{}{"prtg", "web"},
	})

	for _, route := range []struct{ method, path string }{
		{"POST", "/api/read-key/admin/cache/clear"},
		{"POST", "/api/read-key/debug/inject-real-metrics"},
		{"GET", "/web/read-key/dashboard"},
		{"GET", "/api/read-key/config/probes"},
	} {
		if code := surfaceStatus(t, router, route.method, route.path); code != http.StatusNotFound {
			t.Errorf("%s %s answered %d with no administration key, want 404", route.method, route.path, code)
		}
	}

	// What the output exists for is untouched.
	if code := surfaceStatus(t, router, "GET", "/api/read-key/prtg/probes"); code == http.StatusNotFound {
		t.Error("the read surface disappeared with the administration surface")
	}
}

// An unset administration key must never turn an empty or arbitrary
// request into a valid one.
func TestAnUnsetAdministrationKeyMatchesNothing(t *testing.T) {
	base := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	a := NewAuthenticationManager("read-key", "", nil, logger.NewModuleLogger(base, "test.auth"))

	if a.AdminEnabled() {
		t.Error("an empty administration key must not enable the surface")
	}
	for _, provided := range []string{"", "read-key", "anything"} {
		if a.ValidateAdminKey(provided) {
			t.Errorf("%q was accepted as an administration key", provided)
		}
	}
}
