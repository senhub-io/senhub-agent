package http

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestAdminKeyInAnAPIPathWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).Level(zerolog.InfoLevel)
	a := NewAuthenticationManager("read-key", "admin-key", nil, &logger.ModuleLogger{Logger: &zl})

	for _, path := range []string{"/api/admin-key/debug/logs", "/api/admin-key/info/system", "/web/admin-key/"} {
		r := mux.SetURLVars(httptest.NewRequest("GET", path, nil), map[string]string{"agentkey": "admin-key"})
		if _, status := a.resolve(r, true); status != 200 {
			t.Fatalf("%s: status %d, want 200: the key in the path keeps working", path, status)
		}
	}

	out := buf.String()
	if n := strings.Count(out, "deprecated"); n != 1 {
		t.Fatalf("deprecation warnings = %d, want 1:\n%s", n, out)
	}
	if strings.Contains(out, "admin-key/") {
		t.Errorf("the warning carries the key:\n%s", out)
	}
}

func TestConsoleLoginWithTheKeyInThePathDoesNotWarn(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf).Level(zerolog.InfoLevel)
	a := NewAuthenticationManager("read-key", "admin-key", nil, &logger.ModuleLogger{Logger: &zl})

	r := mux.SetURLVars(httptest.NewRequest("GET", "/web/admin-key/", nil), map[string]string{"agentkey": "admin-key"})
	if _, status := a.resolve(r, true); status != 200 {
		t.Fatalf("status %d, want 200", status)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("opening the console warned:\n%s", buf.String())
	}
}
