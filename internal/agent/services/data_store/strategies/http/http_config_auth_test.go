package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// configHandler names the strategy-level entrypoint each /config/* route is
// wired to. These are the handlers that decode the request body and (for
// `test`) reach out to a caller-controlled target — so they must require the
// agent key like every other API route.
type configHandler struct {
	name string
	path string
	fn   func(*HTTPSyncStrategy, http.ResponseWriter, *http.Request)
}

func universalConfigHandlers() []configHandler {
	return []configHandler{
		{"validate", "config/validate", (*HTTPSyncStrategy).handleUniversalConfigValidation},
		{"preview", "config/preview", (*HTTPSyncStrategy).handleUniversalConfigPreview},
		{"test", "config/test", (*HTTPSyncStrategy).handleUniversalConfigTest},
	}
}

func newConfigAuthTestStrategy(agentKey string) *HTTPSyncStrategy {
	base := createTestLoggerForAPI()
	moduleLogger := logger.NewModuleLogger(base, "test.config.auth")
	agentConfig := configuration.NewAgentConfiguration(agentKey, "http://test-server", base)

	strategy := &HTTPSyncStrategy{
		agentConfig: agentConfig,
		logger:      moduleLogger,
		agentKey:    agentKey,
	}
	strategy.authManager = NewAuthenticationManager(agentKey, agentConfig, moduleLogger)
	strategy.configManager = NewConfigurationManager(agentConfig, nil, moduleLogger)
	return strategy
}

// TestUniversalConfigHandlers_RequireAuth is the C1/SSRF regression guard: the
// /config/{validate,preview,test} routes used to decode the body and probe a
// caller-supplied target with no agent-key check, unlike every sibling route.
// An unauthenticated caller must now get 401 before any body is read; a caller
// with the right key must get past auth (here: 400 on an empty body, never a
// silent pass). The pre-fix code returns 400 for the wrong-key case, so the
// 401 assertion fails on it.
func TestUniversalConfigHandlers_RequireAuth(t *testing.T) {
	const agentKey = "test-agent-key"

	for _, h := range universalConfigHandlers() {
		h := h
		t.Run(h.name+"/unauthenticated", func(t *testing.T) {
			strategy := newConfigAuthTestStrategy(agentKey)
			req := httptest.NewRequest(http.MethodPost,
				fmt.Sprintf("/api/%s/%s", "wrong-key", h.path),
				strings.NewReader(`{"probe":"cpu"}`))
			req = mux.SetURLVars(req, map[string]string{"agentkey": "wrong-key"})
			w := httptest.NewRecorder()

			h.fn(strategy, w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s with wrong agent key: got status %d, want 401", h.name, w.Code)
			}
		})

		t.Run(h.name+"/authenticated", func(t *testing.T) {
			strategy := newConfigAuthTestStrategy(agentKey)
			req := httptest.NewRequest(http.MethodPost,
				fmt.Sprintf("/api/%s/%s", agentKey, h.path),
				strings.NewReader(``))
			req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})
			w := httptest.NewRecorder()

			h.fn(strategy, w, req)

			if w.Code == http.StatusUnauthorized {
				t.Errorf("%s with correct agent key: got 401, auth must let a valid key through", h.name)
			}
		})
	}
}

// TestUniversalConfigHandlers_BodyTooLarge pins the DoS bound: an authenticated
// request whose body exceeds maxConfigRequestBytes gets 413, not an unbounded
// buffered decode. The pre-fix handlers decode without a limit and return 200.
func TestUniversalConfigHandlers_BodyTooLarge(t *testing.T) {
	const agentKey = "test-agent-key"
	// Must exceed maxConfigRequestBytes (1 MiB). Kept as a literal so this
	// regression test still compiles against the pre-fix handlers.
	oversized := `{"probe":"` + strings.Repeat("a", (1<<20)+1024) + `"}`

	for _, h := range universalConfigHandlers() {
		h := h
		t.Run(h.name, func(t *testing.T) {
			strategy := newConfigAuthTestStrategy(agentKey)
			req := httptest.NewRequest(http.MethodPost,
				fmt.Sprintf("/api/%s/%s", agentKey, h.path),
				strings.NewReader(oversized))
			req = mux.SetURLVars(req, map[string]string{"agentkey": agentKey})
			w := httptest.NewRecorder()

			h.fn(strategy, w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("%s oversized body: got status %d, want 413", h.name, w.Code)
			}
		})
	}
}
