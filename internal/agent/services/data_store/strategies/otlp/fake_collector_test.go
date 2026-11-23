package otlp

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	collectormetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// fakeCollector is an in-process OTLP receiver, gRPC or HTTP, that can be
// switched down and up. Down answers what a collector that is not there
// answers: UNAVAILABLE over gRPC, 503 over HTTP. It records what it
// acknowledged, in arrival order.
type fakeCollector struct {
	protocol string
	endpoint string

	mu        sync.Mutex
	down      bool
	allow     int // acknowledged requests left before it turns down; -1 = no limit
	reject    bool
	attempts  int
	encodings []string
	metrics   []*collectormetricspb.ExportMetricsServiceRequest
	traces    []*collectortracepb.ExportTraceServiceRequest

	stop func()
}

func newFakeCollector(t *testing.T, protocol string) *fakeCollector {
	t.Helper()
	c := &fakeCollector{protocol: protocol, allow: -1}
	if protocol == "http" {
		srv := httptest.NewServer(http.HandlerFunc(c.serveHTTP))
		c.endpoint = strings.TrimPrefix(srv.URL, "http://")
		c.stop = srv.Close
	} else {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := grpc.NewServer()
		collectormetricspb.RegisterMetricsServiceServer(srv, &fakeMetricsService{c: c})
		collectortracepb.RegisterTraceServiceServer(srv, &fakeTraceService{c: c})
		go func() { _ = srv.Serve(lis) }()
		c.endpoint = lis.Addr().String()
		c.stop = srv.Stop
	}
	t.Cleanup(c.stop)
	return c
}

func (c *fakeCollector) setDown(d bool) {
	c.mu.Lock()
	c.down = d
	c.mu.Unlock()
}

// acknowledgeOnly lets n more requests through, then turns down.
func (c *fakeCollector) acknowledgeOnly(n int) {
	c.mu.Lock()
	c.allow = n
	c.mu.Unlock()
}

func (c *fakeCollector) setReject(r bool) {
	c.mu.Lock()
	c.reject = r
	c.mu.Unlock()
}

func (c *fakeCollector) attemptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

// admit decides one request's fate: 0 accepted, 1 down, 2 refused on its merits.
func (c *fakeCollector) admit() int {
	c.attempts++
	if c.reject {
		return 2
	}
	if c.down || c.allow == 0 {
		return 1
	}
	if c.allow > 0 {
		c.allow--
	}
	return 0
}

func (c *fakeCollector) receivedMetrics() []*collectormetricspb.ExportMetricsServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*collectormetricspb.ExportMetricsServiceRequest(nil), c.metrics...)
}

func (c *fakeCollector) receivedTraces() []*collectortracepb.ExportTraceServiceRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*collectortracepb.ExportTraceServiceRequest(nil), c.traces...)
}

// metricTimes is the timestamp of the first data point of every request
// acknowledged, in order.
func (c *fakeCollector) metricTimes() []int64 {
	var out []int64
	for _, req := range c.receivedMetrics() {
		out = append(out, firstPointTime(req))
	}
	return out
}

func firstPointTime(req *collectormetricspb.ExportMetricsServiceRequest) int64 {
	for _, rm := range req.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				if g := m.GetGauge(); g != nil && len(g.GetDataPoints()) > 0 {
					return int64(g.GetDataPoints()[0].GetTimeUnixNano())
				}
				if g := m.GetSum(); g != nil && len(g.GetDataPoints()) > 0 {
					return int64(g.GetDataPoints()[0].GetTimeUnixNano())
				}
			}
		}
	}
	return 0
}

// spanNames is the name of the first span of every request acknowledged.
func (c *fakeCollector) spanNames() []string {
	var out []string
	for _, req := range c.receivedTraces() {
		name := ""
		for _, rs := range req.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				for _, sp := range ss.GetSpans() {
					if name == "" {
						name = sp.GetName()
					}
				}
			}
		}
		out = append(out, name)
	}
	return out
}

func (c *fakeCollector) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.encodings = append(c.encodings, r.Header.Get("Content-Encoding"))
	c.mu.Unlock()
	if r.Header.Get("Content-Encoding") == "gzip" {
		if zr, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
			body, _ = io.ReadAll(zr)
		}
	}
	c.mu.Lock()
	verdict := c.admit()
	if verdict == 0 {
		switch r.URL.Path {
		case "/v1/metrics":
			var req collectormetricspb.ExportMetricsServiceRequest
			if proto.Unmarshal(body, &req) == nil {
				c.metrics = append(c.metrics, &req)
			}
		case "/v1/traces":
			var req collectortracepb.ExportTraceServiceRequest
			if proto.Unmarshal(body, &req) == nil {
				c.traces = append(c.traces, &req)
			}
		}
	}
	c.mu.Unlock()
	switch verdict {
	case 1:
		http.Error(w, "down", http.StatusServiceUnavailable)
	case 2:
		http.Error(w, "refused", http.StatusBadRequest)
	default:
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}
}

func (c *fakeCollector) grpcVerdict() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.admit() {
	case 1:
		return status.Error(codes.Unavailable, "collector down")
	case 2:
		return status.Error(codes.InvalidArgument, "refused")
	}
	return nil
}

type fakeMetricsService struct {
	collectormetricspb.UnimplementedMetricsServiceServer
	c *fakeCollector
}

func (s *fakeMetricsService) Export(_ context.Context, req *collectormetricspb.ExportMetricsServiceRequest) (*collectormetricspb.ExportMetricsServiceResponse, error) {
	if err := s.c.grpcVerdict(); err != nil {
		return nil, err
	}
	s.c.mu.Lock()
	s.c.metrics = append(s.c.metrics, req)
	s.c.mu.Unlock()
	return &collectormetricspb.ExportMetricsServiceResponse{}, nil
}

type fakeTraceService struct {
	collectortracepb.UnimplementedTraceServiceServer
	c *fakeCollector
}

func (s *fakeTraceService) Export(_ context.Context, req *collectortracepb.ExportTraceServiceRequest) (*collectortracepb.ExportTraceServiceResponse, error) {
	if err := s.c.grpcVerdict(); err != nil {
		return nil, err
	}
	s.c.mu.Lock()
	s.c.traces = append(s.c.traces, req)
	s.c.mu.Unlock()
	return &collectortracepb.ExportTraceServiceResponse{}, nil
}
