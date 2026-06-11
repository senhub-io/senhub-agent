package httpcheck

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, config map[string]interface{}) *HTTPCheckProbe {
	t.Helper()
	probe, err := NewHTTPCheckProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewHTTPCheckProbe: %v", err)
	}
	p, ok := probe.(*HTTPCheckProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("web-edge")
	return p
}

func collectByName(t *testing.T, p *HTTPCheckProbe) map[string]float32 {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]float32{}
	for _, dp := range points {
		got[dp.Name] = dp.Value
		hasTarget := false
		for _, tg := range dp.Tags {
			if tg.Key == "target" && tg.Value != "" {
				hasTarget = true
			}
		}
		if !hasTarget {
			t.Fatalf("datapoint %s missing target tag", dp.Name)
		}
	}
	return got
}

func TestParseConfig_Errors(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"missing targets": {},
		"empty targets":   {"targets": []interface{}{}},
		"bad regexp":      {"targets": []interface{}{"http://x"}, "content_match": "(["},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewHTTPCheckProbe(config, testBaseLogger()); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

// TestCheck_HTTPEndToEnd runs the REAL check path against httptest:
// status, latency phases, response size and content match — all from
// one round trip.
func TestCheck_HTTPEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("service is HEALTHY today")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"targets":       []interface{}{srv.URL},
		"content_match": "HEALTHY",
	})
	got := collectByName(t, p)

	if got["senhub.httpcheck.up"] != 1 {
		t.Errorf("up = %v, want 1", got["senhub.httpcheck.up"])
	}
	if got["senhub.httpcheck.status.code"] != 200 {
		t.Errorf("status.code = %v, want 200", got["senhub.httpcheck.status.code"])
	}
	if got["senhub.httpcheck.response.size"] != 24 {
		t.Errorf("response.size = %v, want 24", got["senhub.httpcheck.response.size"])
	}
	if got["senhub.httpcheck.content.match"] != 1 {
		t.Errorf("content.match = %v, want 1", got["senhub.httpcheck.content.match"])
	}
	if _, ok := got["httpcheck.duration"]; !ok {
		t.Error("missing httpcheck.duration")
	}
	if _, ok := got["senhub.httpcheck.tls.expiry"]; ok {
		t.Error("plain HTTP must not emit tls.expiry")
	}
}

// TestCheck_TLSExpiry runs against httptest's TLS server: the leaf
// certificate's remaining validity must surface as days, and the TLS
// handshake phase must be measured.
func TestCheck_TLSExpiry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"targets":              []interface{}{srv.URL},
		"insecure_skip_verify": true, // httptest's cert is self-signed
	})
	got := collectByName(t, p)

	if got["senhub.httpcheck.up"] != 1 {
		t.Errorf("up = %v, want 1", got["senhub.httpcheck.up"])
	}
	expiry, ok := got["senhub.httpcheck.tls.expiry"]
	if !ok {
		t.Fatal("missing senhub.httpcheck.tls.expiry on a TLS target")
	}
	// httptest certs are long-lived; anything clearly positive is right.
	if expiry <= 0 {
		t.Errorf("tls.expiry = %v days, want > 0", expiry)
	}
	if _, ok := got["senhub.httpcheck.duration.tls"]; !ok {
		t.Error("missing TLS handshake phase duration")
	}
}

// TestCheck_FailureModes pins the measurement semantics: wrong expected
// status, failed content match, and an unreachable target all yield
// up=0 without turning Collect into an error.
func TestCheck_FailureModes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("degraded")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	t.Run("unexpected status", func(t *testing.T) {
		p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{srv.URL}})
		got := collectByName(t, p)
		if got["senhub.httpcheck.up"] != 0 {
			t.Errorf("up = %v, want 0 for 503", got["senhub.httpcheck.up"])
		}
		if got["senhub.httpcheck.status.code"] != 503 {
			t.Errorf("status.code = %v, want 503", got["senhub.httpcheck.status.code"])
		}
	})

	t.Run("expected status override accepts 503", func(t *testing.T) {
		p := newTestProbe(t, map[string]interface{}{
			"targets":         []interface{}{srv.URL},
			"expected_status": 503,
		})
		got := collectByName(t, p)
		if got["senhub.httpcheck.up"] != 1 {
			t.Errorf("up = %v, want 1 with expected_status 503", got["senhub.httpcheck.up"])
		}
	})

	t.Run("content mismatch downs the check", func(t *testing.T) {
		p := newTestProbe(t, map[string]interface{}{
			"targets":         []interface{}{srv.URL},
			"expected_status": 503,
			"content_match":   "HEALTHY",
		})
		got := collectByName(t, p)
		if got["senhub.httpcheck.up"] != 0 {
			t.Errorf("up = %v, want 0 on content mismatch", got["senhub.httpcheck.up"])
		}
		if got["senhub.httpcheck.content.match"] != 0 {
			t.Errorf("content.match = %v, want 0", got["senhub.httpcheck.content.match"])
		}
	})

	t.Run("unreachable target is a measurement", func(t *testing.T) {
		p := newTestProbe(t, map[string]interface{}{
			"targets": []interface{}{"http://127.0.0.1:1"},
			"timeout": 1,
		})
		got := collectByName(t, p)
		if got["senhub.httpcheck.up"] != 0 {
			t.Errorf("up = %v, want 0", got["senhub.httpcheck.up"])
		}
		if _, ok := got["senhub.httpcheck.status.code"]; ok {
			t.Error("unreachable target must not emit a status code")
		}
	})
}

func TestCheck_RedirectIsMeasuredNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/next", http.StatusMovedPermanently)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{srv.URL}})
	got := collectByName(t, p)
	if got["senhub.httpcheck.status.code"] != 301 {
		t.Errorf("status.code = %v, want 301 (redirects are not followed)", got["senhub.httpcheck.status.code"])
	}
	if got["senhub.httpcheck.up"] != 1 {
		t.Errorf("up = %v, want 1 (3xx is in the default healthy range)", got["senhub.httpcheck.up"])
	}
}

func TestCollect_SeamForChassis(t *testing.T) {
	// The chassis seam stays available for deterministic fan-out tests.
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"http://a", "http://b"}})
	var calls atomic.Int32
	p.check = func(target string) httpResult {
		calls.Add(1)
		return httpResult{target: target, up: true, statusCode: 200, tlsDaysLeft: noTLSSentinel}
	}
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("check called %d times, want 2", calls.Load())
	}
}

func TestTargetsTagDiscriminates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{srv.URL, srv.URL + "/two"}})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	targets := map[string]bool{}
	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "target" {
				targets[tg.Value] = true
			}
		}
	}
	if len(targets) != 2 {
		t.Errorf("distinct targets in tags = %d, want 2", len(targets))
	}
	if !strings.HasSuffix(pickOne(targets), "") {
		t.Log("targets:", targets)
	}
}

func pickOne(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}
