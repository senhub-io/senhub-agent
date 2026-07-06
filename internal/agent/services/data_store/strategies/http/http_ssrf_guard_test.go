package http

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		{"fd00:ec2::254", true},   // AWS IMDS over IPv6 — unique-local, NOT link-local
		{"168.63.129.16", true},   // Azure WireServer — public-range metadata pivot
		{"127.0.0.1", false},      // loopback: a colocated probe target is legitimate
		{"10.0.0.5", false},       // RFC1918: internal monitoring is the product's job
		{"192.168.1.1", false},    // RFC1918
		{"172.16.4.4", false},     // RFC1918
		{"fd12:3456::1", false},   // other unique-local: internal monitoring is legitimate
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

// TestParseHostIP_StripsIPv6Zone ensures a zoned IPv6 literal (e.g. the host
// carried by a URL like http://[fe80::1%25eth0]/) still parses to its concrete
// IP so the block list applies. net.ParseIP alone returns nil on a zoned
// literal, which pre-fix let fe80::1%eth0 bypass the fe80 block entirely.
func TestParseHostIP_StripsIPv6Zone(t *testing.T) {
	ip := parseHostIP("fe80::1%eth0")
	if ip == nil {
		t.Fatal("parseHostIP returned nil for a zoned IPv6 literal")
	}
	if !blockedConnectivityIP(ip) {
		t.Errorf("zoned fe80::1%%eth0 must be blocked, parsed as %v", ip)
	}
}

// TestConnectivityTest_DoesNotFollowRedirect ensures an allowed target cannot
// 30x-bounce the connectivity probe onto another address: the redirect
// Location is never dialed. The target itself is still reported reachable — a
// http→https 301 is legitimate and near-universal — because the dial-time
// guard already re-checks every hop, so hard-failing every redirect was a
// functional regression.
func TestConnectivityTest_DoesNotFollowRedirect(t *testing.T) {
	var okHit int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&okHit, 1)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ok", http.StatusMovedPermanently)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cm := newConnGuardTestCM()
	config := map[string]interface{}{"url": srv.URL + "/redir"}

	result := cm.testWebAppConnectivity(config, 5)

	if !result.Passed {
		t.Errorf("a redirecting-but-reachable target must pass; got error %q", result.Error)
	}
	if n := atomic.LoadInt32(&okHit); n != 0 {
		t.Errorf("redirect was followed: /ok hit %d times, want 0", n)
	}
}
