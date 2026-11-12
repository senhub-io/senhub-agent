package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A console check whose request was dropped must stop dialing the target:
// the connectivity test ends with the request, not with its own timeout.
func TestConnectivityTest_EndsWithTheRequestContext(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	cm := newConnGuardTestCM()
	cases := map[string]func(ctx context.Context) ValidationTestResult{
		"webapp": func(ctx context.Context) ValidationTestResult {
			return cm.testWebAppConnectivity(ctx, map[string]interface{}{"url": srv.URL}, 30)
		},
		"redfish": func(ctx context.Context) ValidationTestResult {
			return cm.testRedfishConnectivity(ctx, map[string]interface{}{"endpoint": srv.URL}, 30)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(50*time.Millisecond, cancel)
			started := time.Now()
			result := run(ctx)
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("the connectivity test took %v after its context was cancelled", elapsed)
			}
			if result.Passed || !strings.Contains(result.Error, "context canceled") {
				t.Fatalf("result = %+v, want a failure naming the cancellation", result)
			}
		})
	}
}
