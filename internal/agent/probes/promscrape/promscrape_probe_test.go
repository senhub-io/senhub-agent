package promscrape

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, config map[string]interface{}) *PromScrapeProbe {
	t.Helper()
	probe, err := NewPromScrapeProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewPromScrapeProbe: %v", err)
	}
	p, ok := probe.(*PromScrapeProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("edge-scraper")
	return p
}

const exposition = `# HELP node_cpu_seconds_total Seconds the CPUs spent in each mode.
# TYPE node_cpu_seconds_total counter
node_cpu_seconds_total{cpu="0",mode="idle"} 100.5
node_cpu_seconds_total{cpu="1",mode="idle"} 200.25
# HELP node_load1 1m load average.
# TYPE node_load1 gauge
node_load1 0.82
# HELP http_request_duration_seconds Request latency.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.1"} 100
http_request_duration_seconds_bucket{le="+Inf"} 120
http_request_duration_seconds_sum 9.2
http_request_duration_seconds_count 120
# TYPE custom_untyped untyped
custom_untyped 42
`

func collectFrom(t *testing.T, p *PromScrapeProbe) []data_store.DataPoint {
	t.Helper()
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return points
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

// TestScrape_EndToEnd runs the REAL scrape path against httptest:
// counters and gauges flow through with labels and otel_type, untyped
// becomes gauge, histogram series are dropped and counted.
func TestScrape_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if _, err := w.Write([]byte(exposition)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{srv.URL}})
	got := indexByName(collectFrom(t, p))

	if up := got["senhub.promscrape.up"]; len(up) != 1 || up[0].Value != 1 {
		t.Fatalf("up = %+v, want one point with value 1", up)
	}
	if samples := got["senhub.promscrape.samples"]; len(samples) != 1 || samples[0].Value != 4 {
		t.Errorf("samples = %+v, want 4 (2 counter + 1 gauge + 1 untyped)", samples)
	}
	if dropped := got["senhub.promscrape.dropped"]; len(dropped) != 1 || dropped[0].Value != 1 {
		t.Errorf("dropped = %+v, want 1 (histogram family)", dropped)
	}

	cpus := got["node_cpu_seconds_total"]
	if len(cpus) != 2 {
		t.Fatalf("node_cpu_seconds_total series = %d, want 2", len(cpus))
	}
	for _, dp := range cpus {
		if tagValue(dp, "otel_type") != "counter" {
			t.Errorf("counter sample missing otel_type=counter: %+v", dp.Tags)
		}
		if tagValue(dp, "cpu") == "" || tagValue(dp, "mode") != "idle" {
			t.Errorf("scraped labels not preserved as tags: %+v", dp.Tags)
		}
		if tagValue(dp, "target") != srv.URL {
			t.Errorf("missing target tag: %+v", dp.Tags)
		}
	}

	if load := got["node_load1"]; len(load) != 1 || tagValue(load[0], "otel_type") != "gauge" {
		t.Errorf("node_load1 = %+v, want one gauge", load)
	}
	if untyped := got["custom_untyped"]; len(untyped) != 1 || tagValue(untyped[0], "otel_type") != "gauge" {
		t.Errorf("custom_untyped = %+v, want one gauge (untyped maps to gauge)", untyped)
	}
	if hist := got["http_request_duration_seconds_bucket"]; len(hist) != 0 {
		t.Errorf("histogram series must be dropped, got %+v", hist)
	}
}

// TestScrape_FailureIsAMeasurement pins the chassis semantics: an
// unreachable exporter yields up=0 without turning Collect into an
// error, and emits no scrape-quality metrics.
func TestScrape_FailureIsAMeasurement(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{
		"targets": []interface{}{"http://127.0.0.1:1/metrics"},
		"timeout": 1,
	})
	got := indexByName(collectFrom(t, p))

	if up := got["senhub.promscrape.up"]; len(up) != 1 || up[0].Value != 0 {
		t.Fatalf("up = %+v, want one point with value 0", up)
	}
	if _, ok := got["senhub.promscrape.samples"]; ok {
		t.Error("failed scrape must not emit a samples count")
	}
}

func TestScrape_NonOKStatusIsDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{srv.URL}})
	got := indexByName(collectFrom(t, p))
	if up := got["senhub.promscrape.up"]; len(up) != 1 || up[0].Value != 0 {
		t.Errorf("up = %+v, want 0 on HTTP 503", up)
	}
}

func TestScrape_MetricMatchFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(exposition)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"targets":      []interface{}{srv.URL},
		"metric_match": "^node_",
	})
	got := indexByName(collectFrom(t, p))

	if _, ok := got["custom_untyped"]; ok {
		t.Error("metric_match must filter non-matching families")
	}
	if len(got["node_cpu_seconds_total"]) != 2 {
		t.Errorf("matching families must survive the filter")
	}
	if samples := got["senhub.promscrape.samples"]; len(samples) != 1 || samples[0].Value != 3 {
		t.Errorf("samples = %+v, want 3 after filtering", samples)
	}
}

func TestScrape_BearerTokenSent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if _, err := w.Write([]byte("up 1\n")); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"targets":      []interface{}{srv.URL},
		"bearer_token": "s3cret",
	})
	collectFrom(t, p)
	if gotAuth != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want Bearer s3cret", gotAuth)
	}
}

func TestCollect_SeamForChassis(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{"http://a/metrics", "http://b/metrics"}})
	var calls atomic.Int32
	p.scrape = func(target string) scrapeResult {
		calls.Add(1)
		return scrapeResult{target: target}
	}
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("scrape called %d times, want 2", calls.Load())
	}
}

func TestParseConfig_Errors(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"missing targets": {},
		"empty targets":   {"targets": []interface{}{}},
		"bad regexp":      {"targets": []interface{}{"http://x"}, "metric_match": "(["},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPromScrapeProbe(config, testBaseLogger()); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}
