package solr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

// metricsPayload returns a minimal Solr /admin/metrics?wt=json&group=all response.
func metricsPayload() map[string]interface{} {
	return map[string]interface{}{
		"metrics": map[string]interface{}{
			"solr.jvm": map[string]interface{}{
				"memory": map[string]interface{}{
					"heap": map[string]interface{}{
						"used": 123456789.0,
					},
				},
				"threads": map[string]interface{}{
					"count": 42.0,
				},
			},
			"solr.node": map[string]interface{}{
				"QUERY./select.requestTimes": map[string]interface{}{
					"count":  100.0,
					"meanMs": 5.0,
				},
				"QUERY./select.errors": map[string]interface{}{
					"count": 3.0,
				},
				"CACHE.searcher.queryResultCache": map[string]interface{}{
					"hits":    50.0,
					"inserts": 25.0,
				},
			},
			"solr.core.collection1": map[string]interface{}{
				"INDEX": map[string]interface{}{
					"sizeInBytes": 8192.0,
				},
			},
		},
	}
}

// coresPayload returns a minimal Solr /admin/cores?action=STATUS&wt=json response.
func coresPayload() map[string]interface{} {
	return map[string]interface{}{
		"status": map[string]interface{}{
			"collection1": map[string]interface{}{
				"index": map[string]interface{}{
					"numDocs": 1000.0,
				},
			},
		},
	}
}

func TestNewSolrProbe_Defaults(t *testing.T) {
	p, err := NewSolrProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	sp := p.(*SolrProbe)
	if sp.config.Endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", sp.config.Endpoint, defaultEndpoint)
	}
	if sp.config.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", sp.config.Timeout, defaultTimeout)
	}
	if sp.config.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", sp.config.Interval, defaultInterval)
	}
	if sp.GetProbeType() != ProbeType {
		t.Errorf("probe type = %q, want %q", sp.GetProbeType(), ProbeType)
	}
}

func TestNewSolrProbe_CustomConfig(t *testing.T) {
	p, err := NewSolrProbe(map[string]interface{}{
		"endpoint": "http://mysolr:8983",
		"timeout":  5,
		"interval": 30,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	sp := p.(*SolrProbe)
	if sp.config.Endpoint != "http://mysolr:8983" {
		t.Errorf("endpoint = %q, want http://mysolr:8983", sp.config.Endpoint)
	}
}

func TestNewSolrProbe_JolokiaURL(t *testing.T) {
	// jolokia_url with /solr/admin/metrics suffix should be stripped to the base.
	p, err := NewSolrProbe(map[string]interface{}{
		"jolokia_url": "http://localhost:8983/solr/admin/metrics?wt=json",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	sp := p.(*SolrProbe)
	if sp.config.Endpoint != "http://localhost:8983" {
		t.Errorf("endpoint = %q, want http://localhost:8983", sp.config.Endpoint)
	}
}

// TestCollect_Up verifies that a successful collection emits senhub.solr.up=1
// plus the expected metric set.
func TestCollect_Up(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/solr/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metricsPayload())
	})
	mux.HandleFunc("/solr/admin/cores", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(coresPayload())
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	p, err := NewSolrProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	p.(*SolrProbe).SetName("solr-test")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	checks := map[string]float32{
		"senhub.solr.up":     1,
		"jvm.memory.heap.used": 123456789,
		"jvm.threads.count":  42,
		"solr.requests.count": 100,
		"solr.requests.time":  500, // 100 * 5ms
		"solr.errors.count":   3,
		"solr.cache.hits":     50,
		"solr.cache.inserts":  25,
		"solr.index.size":     8192,
		"solr.document.count": 1000,
	}

	for name, want := range checks {
		got, ok := byName[name]
		if !ok {
			t.Errorf("metric %q not emitted", name)
			continue
		}
		if got != want {
			t.Errorf("metric %q = %v, want %v", name, got, want)
		}
	}
}

// TestCollect_Down verifies that an unreachable Solr emits senhub.solr.up=0
// and Collect() returns nil (no error).
func TestCollect_Down(t *testing.T) {
	p, err := NewSolrProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:19876", // port that should refuse connection
		"timeout":  1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	p.(*SolrProbe).SetName("solr-down")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect returned error, want nil: %v", err)
	}

	var upVal *float32
	for _, dp := range points {
		if dp.Name == "senhub.solr.up" {
			v := dp.Value
			upVal = &v
			break
		}
	}
	if upVal == nil {
		t.Fatal("senhub.solr.up not emitted")
	}
	if *upVal != 0 {
		t.Errorf("senhub.solr.up = %v, want 0", *upVal)
	}
}

// TestCollect_CoreTagPresent verifies that per-core metrics carry the core tag.
func TestCollect_CoreTagPresent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/solr/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(metricsPayload())
	})
	mux.HandleFunc("/solr/admin/cores", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(coresPayload())
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	p, err := NewSolrProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewSolrProbe: %v", err)
	}
	p.(*SolrProbe).SetName("solr-test")

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, dp := range points {
		if dp.Name != "solr.index.size" && dp.Name != "solr.document.count" {
			continue
		}
		var coreTag string
		for _, tg := range dp.Tags {
			if tg.Key == "core" {
				coreTag = tg.Value
				break
			}
		}
		if coreTag == "" {
			t.Errorf("metric %q missing core tag", dp.Name)
		}
		if coreTag != "collection1" {
			t.Errorf("metric %q core tag = %q, want collection1", dp.Name, coreTag)
		}
	}
}

func TestExtractNestedFloat(t *testing.T) {
	m := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": 3.14,
			},
		},
		"x": 42.0,
	}

	cases := []struct {
		path string
		want float64
	}{
		{"a.b.c", 3.14},
		{"x", 42.0},
		{"missing", -1},
		{"a.missing", -1},
	}

	for _, tc := range cases {
		got := extractNestedFloat(m, tc.path)
		if got != tc.want {
			t.Errorf("extractNestedFloat(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
