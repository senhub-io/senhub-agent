package pulsar

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func testTS() time.Time { return time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC) }

func newTestProbe(t *testing.T, config map[string]interface{}) *PulsarProbe {
	t.Helper()
	probe, err := NewPulsarProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewPulsarProbe: %v", err)
	}
	p, ok := probe.(*PulsarProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("pulsar-test")
	return p
}

func indexByName(points []data_store.DataPoint) map[string][]data_store.DataPoint {
	got := map[string][]data_store.DataPoint{}
	for _, dp := range points {
		got[dp.Name] = append(got[dp.Name], dp)
	}
	return got
}

func tagValue(dp data_store.DataPoint, key string) string {
	for _, tg := range dp.Tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

// brokerMetrics is a realistic subset of the Prometheus text exposition
// that a Pulsar broker emits at GET /metrics.
const brokerMetrics = `# HELP pulsar_topics_count Number of topics
# TYPE pulsar_topics_count gauge
pulsar_topics_count{namespace="public/default"} 42
# HELP pulsar_producers_count Number of producers
# TYPE pulsar_producers_count gauge
pulsar_producers_count{namespace="public/default"} 10
# HELP pulsar_consumers_count Number of consumers
# TYPE pulsar_consumers_count gauge
pulsar_consumers_count{namespace="public/default"} 20
# HELP pulsar_rate_in Messages in per second
# TYPE pulsar_rate_in gauge
pulsar_rate_in{namespace="public/default"} 500.5
# HELP pulsar_rate_out Messages out per second
# TYPE pulsar_rate_out gauge
pulsar_rate_out{namespace="public/default"} 480.0
# HELP pulsar_throughput_in Bytes in per second
# TYPE pulsar_throughput_in gauge
pulsar_throughput_in{namespace="public/default"} 1024000
# HELP pulsar_throughput_out Bytes out per second
# TYPE pulsar_throughput_out gauge
pulsar_throughput_out{namespace="public/default"} 980000
# HELP pulsar_storage_size Storage size in bytes
# TYPE pulsar_storage_size gauge
pulsar_storage_size{namespace="public/default"} 5368709120
# HELP pulsar_msg_backlog Message backlog
# TYPE pulsar_msg_backlog gauge
pulsar_msg_backlog{namespace="public/default"} 100
# HELP pulsar_storage_read_rate Storage read rate
# TYPE pulsar_storage_read_rate gauge
pulsar_storage_read_rate{namespace="public/default"} 300
# HELP pulsar_storage_write_rate Storage write rate
# TYPE pulsar_storage_write_rate gauge
pulsar_storage_write_rate{namespace="public/default"} 310
# HELP ignored_metric Something we do not track
# TYPE ignored_metric gauge
ignored_metric 99
`

// TestCollect_EndToEnd starts a fake Pulsar server (ready + metrics),
// collects, and verifies the full set of emitted datapoints.
func TestCollect_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v2/brokers/ready":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("status-servlet,brokerconn-servlet,pulsar-io-servlet"))
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte(brokerMetrics))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := indexByName(points)

	// Availability gauge must be 1 (broker ready).
	if up := got["senhub.pulsar.up"]; len(up) != 1 || up[0].Value != 1 {
		t.Errorf("senhub.pulsar.up = %+v, want one point with value 1", up)
	}

	// All 11 tracked metrics must appear.
	wantNames := []string{
		"pulsar.topics.count",
		"pulsar.producers.count",
		"pulsar.consumers.count",
		"pulsar.rate.messages.in",
		"pulsar.rate.messages.out",
		"pulsar.throughput.in",
		"pulsar.throughput.out",
		"pulsar.storage.size",
		"pulsar.message.backlog",
		"pulsar.storage.read.rate",
		"pulsar.storage.write.rate",
	}
	for _, name := range wantNames {
		if len(got[name]) == 0 {
			t.Errorf("missing metric %q", name)
		}
	}

	// Verify a concrete value and that labels survive as tags.
	if topics := got["pulsar.topics.count"]; len(topics) == 1 {
		if topics[0].Value != 42 {
			t.Errorf("pulsar.topics.count = %v, want 42", topics[0].Value)
		}
		if ns := tagValue(topics[0], "namespace"); ns != "public/default" {
			t.Errorf("namespace tag = %q, want public/default", ns)
		}
	}

	// Untracked metrics must NOT appear.
	if _, ok := got["ignored_metric"]; ok {
		t.Error("untracked metric must not be emitted")
	}

	// The endpoint tag must propagate.
	if up := got["senhub.pulsar.up"]; len(up) > 0 {
		if ep := tagValue(up[0], "endpoint"); !strings.HasPrefix(ep, "http://127.0.0.1") {
			t.Errorf("endpoint tag = %q, want the test server URL", ep)
		}
	}

	// probe_name must be set (enrichment contract).
	if up := got["senhub.pulsar.up"]; len(up) > 0 {
		if pn := tagValue(up[0], "probe_name"); pn != "pulsar-test" {
			t.Errorf("probe_name = %q, want pulsar-test", pn)
		}
	}
}

// TestCollect_DownBroker verifies that an unreachable broker yields
// senhub.pulsar.up = 0 and no metric datapoints, and that Collect
// does not return an error (down is a measurement, not a failure).
func TestCollect_DownBroker(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"endpoint": "http://127.0.0.1:1",
		"timeout":  1,
	})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not error on unreachable broker: %v", err)
	}
	got := indexByName(points)

	if up := got["senhub.pulsar.up"]; len(up) != 1 || up[0].Value != 0 {
		t.Errorf("senhub.pulsar.up = %+v, want value 0 for unreachable broker", up)
	}
	for _, name := range []string{"pulsar.topics.count", "pulsar.rate.messages.in"} {
		if len(got[name]) > 0 {
			t.Errorf("%q must not be emitted when broker is down", name)
		}
	}
}

// TestCollect_ReadyCheckFails verifies that a 503 from /ready results
// in up=0 without attempting /metrics.
func TestCollect_ReadyCheckFails(t *testing.T) {
	metricsCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v2/brokers/ready":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/metrics":
			metricsCalled = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"endpoint": srv.URL})
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := indexByName(points)

	if up := got["senhub.pulsar.up"]; len(up) != 1 || up[0].Value != 0 {
		t.Errorf("senhub.pulsar.up = %+v, want 0 on 503 ready check", up)
	}
	if metricsCalled {
		t.Error("/metrics must not be scraped when broker is not ready")
	}
}

// TestParsePrometheusText_NoLabels verifies that a bare metric line
// (no label set) is parsed correctly.
func TestParsePrometheusText_NoLabels(t *testing.T) {
	input := "pulsar_topics_count 7\n"
	got, err := parsePrometheusText(strings.NewReader(input), testTS(), nil)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(points) = %d, want 1", len(got))
	}
	if got[0].Name != "pulsar.topics.count" {
		t.Errorf("Name = %q, want pulsar.topics.count", got[0].Name)
	}
	if got[0].Value != 7 {
		t.Errorf("Value = %v, want 7", got[0].Value)
	}
}

// TestParsePrometheusText_IgnoresUnknown confirms untracked metrics
// are silently dropped.
func TestParsePrometheusText_IgnoresUnknown(t *testing.T) {
	input := "unknown_metric{foo=\"bar\"} 99\npulsar_msg_backlog 3\n"
	got, err := parsePrometheusText(strings.NewReader(input), testTS(), nil)
	if err != nil {
		t.Fatalf("parsePrometheusText: %v", err)
	}
	if len(got) != 1 || got[0].Name != "pulsar.message.backlog" {
		t.Errorf("unexpected points: %+v", got)
	}
}

// TestNewPulsarProbe_Defaults verifies that omitting all optional
// config keys leaves the probe with the documented defaults.
func TestNewPulsarProbe_Defaults(t *testing.T) {
	probe, err := NewPulsarProbe(map[string]interface{}{}, testBaseLogger())
	if err != nil {
		t.Fatalf("NewPulsarProbe: %v", err)
	}
	p := probe.(*PulsarProbe)
	if p.config.Endpoint != defaultEndpoint {
		t.Errorf("Endpoint = %q, want %q", p.config.Endpoint, defaultEndpoint)
	}
	if p.config.Timeout != defaultTimeout {
		t.Errorf("Timeout = %v, want %v", p.config.Timeout, defaultTimeout)
	}
	if p.config.Interval != defaultInterval {
		t.Errorf("Interval = %v, want %v", p.config.Interval, defaultInterval)
	}
}

// TestAppendLabelTags validates the label parser against common forms.
func TestAppendLabelTags(t *testing.T) {
	tests := []struct {
		name     string
		labelStr string
		wantKeys []string
		wantVals []string
	}{
		{
			name:     "single label",
			labelStr: `namespace="public/default"`,
			wantKeys: []string{"namespace"},
			wantVals: []string{"public/default"},
		},
		{
			name:     "two labels",
			labelStr: `a="1",b="2"`,
			wantKeys: []string{"a", "b"},
			wantVals: []string{"1", "2"},
		},
		{
			name:     "empty string",
			labelStr: ``,
			wantKeys: nil,
			wantVals: nil,
		},
		{
			name:     "malformed pair",
			labelStr: `malformed`,
			wantKeys: nil,
			wantVals: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendLabelTags(nil, tt.labelStr)
			if len(got) != len(tt.wantKeys) {
				t.Fatalf("len = %d, want %d (tags: %v)", len(got), len(tt.wantKeys), got)
			}
			for i, tg := range got {
				if tg.Key != tt.wantKeys[i] || tg.Value != tt.wantVals[i] {
					t.Errorf("tag[%d] = {%s=%s}, want {%s=%s}", i, tg.Key, tg.Value, tt.wantKeys[i], tt.wantVals[i])
				}
			}
		})
	}
}
