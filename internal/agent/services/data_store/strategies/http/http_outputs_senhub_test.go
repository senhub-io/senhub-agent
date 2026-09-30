package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
)

// fakeIntake answers like the SenHub intake: /status is public, the
// other routes demand a known X-AGENT-KEY, anything else is a 404.
func fakeIntake(t *testing.T, knownKey string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.WriteHeader(http.StatusOK)
		case "/configs":
			if r.Header.Get("X-AGENT-KEY") != knownKey {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func withIntakeURL(t *testing.T, url string) {
	t.Helper()
	prev := cliArgs.ProductionURL
	cliArgs.ProductionURL = url
	t.Cleanup(func() { cliArgs.ProductionURL = prev })
}

func TestOutputTest_SenhubPassesOnlyWithAnAcceptedKey(t *testing.T) {
	router, _ := newOutputsTestRouter(t)
	base := "/api/" + testAdminKey

	withIntakeURL(t, fakeIntake(t, "test-agent-key").URL)
	code, resp := doJSON(t, router, "POST", base+"/config/outputs/test", map[string]interface{}{"type": "senhub", "params": map[string]interface{}{}, "timeout": 3})
	if code != 200 || resp["valid"] != true {
		t.Fatalf("an intake that accepts the agent key must pass, got %d %v", code, resp)
	}

	withIntakeURL(t, fakeIntake(t, "another-agent").URL)
	_, resp = doJSON(t, router, "POST", base+"/config/outputs/test", map[string]interface{}{"type": "senhub", "params": map[string]interface{}{}, "timeout": 3})
	if resp["valid"] != false {
		t.Fatalf("an intake that refuses the agent key must fail, got %v", resp)
	}
	steps := resp["steps"].([]interface{})
	if last := steps[len(steps)-1].(map[string]interface{}); last["name"] != "authenticate" {
		t.Errorf("the failing step must be authenticate, got %v", last)
	}
}

// The recette saw {"valid":true,"steps":[{"name":"reach","passed":true,
// "detail":"...: HTTP 404"}]}: any answer counted as a working output.
func TestOutputTest_SenhubFailsOnA404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	steps := probeSenhubIntake(context.Background(), srv.URL, "k", 3*time.Second)
	last := steps[len(steps)-1]
	if last.Passed || last.Name != "reach" || !strings.Contains(last.Error, "404") {
		t.Errorf("a 404 must fail the reach step, got %+v", steps)
	}
}

func TestOutputTest_SenhubFailsWithoutAnAgentKey(t *testing.T) {
	srv := fakeIntake(t, "k")
	steps := probeSenhubIntake(context.Background(), srv.URL, "", 3*time.Second)
	if len(steps) != 1 || steps[0].Passed || steps[0].Name != "config" || !strings.Contains(steps[0].Error, "agent key") {
		t.Errorf("a missing agent key must fail the config step, got %+v", steps)
	}
}
