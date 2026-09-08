package otlp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func plainDialer(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

func TestProbeConnection_HTTPReceiverAcceptsOneMetric(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	endpoint := strings.TrimPrefix(srv.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	steps := ProbeConnection(ctx, map[string]interface{}{
		"endpoint": endpoint, "protocol": "http",
		"tls":   map[string]interface{}{"enabled": false},
		"retry": map[string]interface{}{"enabled": false},
	}, plainDialer)
	names := []string{}
	for _, s := range steps {
		names = append(names, s.Name)
		if !s.Passed {
			t.Errorf("step %s failed: %s", s.Name, s.Error)
		}
	}
	if strings.Join(names, ",") != "config,dns,tcp,export" {
		t.Errorf("steps: %v", names)
	}
	if hits.Load() != 1 {
		t.Errorf("the receiver must see exactly one export, saw %d", hits.Load())
	}
}

func TestProbeConnection_StopsAtTheFirstFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	steps := ProbeConnection(ctx, map[string]interface{}{"endpoint": "127.0.0.1:1", "protocol": "http", "tls": map[string]interface{}{"enabled": false}}, plainDialer)
	last := steps[len(steps)-1]
	if last.Name != "tcp" || last.Passed {
		t.Errorf("a closed port must fail at tcp, got %+v", steps)
	}
	steps = ProbeConnection(ctx, map[string]interface{}{"protocol": "carrier-pigeon"}, plainDialer)
	if len(steps) != 1 || steps[0].Name != "config" || steps[0].Passed {
		t.Errorf("a bad config must fail at config, got %+v", steps)
	}
	blocked := func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, context.Canceled
	}
	steps = ProbeConnection(ctx, map[string]interface{}{"endpoint": "localhost:4317"}, blocked)
	if last := steps[len(steps)-1]; last.Name != "tcp" || last.Passed {
		t.Errorf("the dialer's refusal must surface at tcp, got %+v", steps)
	}
}
