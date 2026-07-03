package http

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

func newConnGuardTestCM() *ConfigurationManager {
	base := createTestLoggerForAPI()
	moduleLogger := logger.NewModuleLogger(base, "test.ssrf.guard")
	agentConfig := configuration.NewAgentConfiguration("test-agent-key", "http://test-server", base)
	return NewConfigurationManager(agentConfig, nil, moduleLogger)
}

func TestBlockedConnectivityIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"169.254.169.254", true}, // cloud instance-metadata (AWS/GCP/Azure/OpenStack)
		{"169.254.0.1", true},     // link-local generally
		{"fe80::1", true},         // IPv6 link-local
		{"127.0.0.1", false},      // loopback: a colocated probe target is legitimate
		{"10.0.0.5", false},       // RFC1918: internal monitoring is the product's job
		{"192.168.1.1", false},    // RFC1918
		{"172.16.4.4", false},     // RFC1918
		{"8.8.8.8", false},        // public
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := blockedConnectivityIP(ip); got != c.blocked {
			t.Errorf("blockedConnectivityIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

// TestConnectivityTest_BlocksCloudMetadata is the SSRF regression guard: the
// /config/test connectivity check must refuse to dial the cloud metadata
// address. The pre-fix client dials it directly, so its error is an ordinary
// "connectivity test failed" (unreachable/timeout), never a block — that is the
// FAIL-on-old signal.
func TestConnectivityTest_BlocksCloudMetadata(t *testing.T) {
	cm := newConnGuardTestCM()
	config := map[string]interface{}{"url": "http://169.254.169.254/latest/meta-data/"}

	result := cm.testWebAppConnectivity(config, 2)

	if result.Passed {
		t.Fatalf("connectivity test to 169.254.169.254 passed; metadata endpoint must be blocked")
	}
	if !strings.Contains(result.Error, "blocked") {
		t.Errorf("expected a block error, got %q", result.Error)
	}
}

// TestConnectivityTest_DoesNotFollowRedirect ensures an allowed target cannot
// 30x-bounce the connectivity probe onto another address. The pre-fix client
// follows the redirect to the 200 endpoint and reports the target reachable.
func TestConnectivityTest_DoesNotFollowRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cm := newConnGuardTestCM()
	config := map[string]interface{}{"url": srv.URL + "/redir"}

	result := cm.testWebAppConnectivity(config, 5)

	if result.Passed {
		t.Errorf("connectivity test followed a redirect; redirects must not be followed")
	}
}
