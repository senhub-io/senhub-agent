package activemq

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testBaseLogger() *logger.Logger {
	return logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
}

func newTestProbe(t *testing.T, config map[string]interface{}) *activemqProbe {
	t.Helper()
	probe, err := NewActivemqProbe(config, testBaseLogger())
	if err != nil {
		t.Fatalf("NewActivemqProbe: %v", err)
	}
	p, ok := probe.(*activemqProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	p.SetName("activemq-test")
	return p
}

// jolokiaServer builds a minimal httptest.Server that fakes Jolokia responses
// for the ActiveMQ broker MBeans used by this probe. The handler inspects the
// URL path to determine which metric is being requested and returns a synthetic
// JSON response.
func jolokiaServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Broker-level attributes
	brokerAttrs := map[string]interface{}{
		"TotalProducerCount": json.Number("3"),
		"TotalConsumerCount": json.Number("7"),
		"TotalMessageCount":  json.Number("42"),
		"MemoryPercentUsage": json.Number("20"),
		"StorePercentUsage":  json.Number("35"),
		"TempPercentUsage":   json.Number("5"),
		"BrokerId":           "test-broker-uuid-1234",
	}

	// Queue attributes (per-destination)
	queueAttrs := map[string]interface{}{
		"EnqueueCount":  json.Number("100"),
		"DequeueCount":  json.Number("80"),
		"QueueSize":     json.Number("20"),
		"ConsumerCount": json.Number("2"),
		"ProducerCount": json.Number("1"),
	}

	jolokiaOK := func(w http.ResponseWriter, value interface{}) {
		raw, _ := json.Marshal(value)
		fmt.Fprintf(w, `{"value":%s,"status":200}`, raw)
	}

	mux.HandleFunc("/api/jolokia/read/", func(w http.ResponseWriter, r *http.Request) {
		// Path format: /api/jolokia/read/<mbean>/<attribute>
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/jolokia/read/"), "/", 2)
		if len(parts) != 2 {
			http.Error(w, "bad path", 400)
			return
		}
		attr := parts[1]

		// Destination-level read
		if strings.Contains(parts[0], "destinationName") {
			if v, ok := queueAttrs[attr]; ok {
				jolokiaOK(w, v)
			} else {
				fmt.Fprintf(w, `{"status":404,"error":"attribute not found"}`)
			}
			return
		}

		// Broker-level read
		if v, ok := brokerAttrs[attr]; ok {
			jolokiaOK(w, v)
		} else {
			fmt.Fprintf(w, `{"status":404,"error":"attribute not found"}`)
		}
	})

	// List endpoint for queues — returns two queue names.
	mux.HandleFunc("/api/jolokia/list/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "destinationType%3DQueue") || strings.Contains(r.URL.Path, "destinationType=Queue") {
			fmt.Fprintf(w, `{"value":{"orders":{},"shipping":{}},"status":200}`)
			return
		}
		// Topics — empty for simplicity.
		fmt.Fprintf(w, `{"value":{},"status":200}`)
	})

	return httptest.NewServer(mux)
}

func TestCollect_BrokerUp(t *testing.T) {
	srv := jolokiaServer(t)
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"jolokia_url": srv.URL + "/api/jolokia",
		"username":    "",
		"password":    "",
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Index broker-level datapoints by metric name (those without a destination tag).
	brokerByName := map[string]float32{}
	for _, dp := range points {
		isDestLevel := false
		for _, tg := range dp.Tags {
			if tg.Key == "destination" {
				isDestLevel = true
				break
			}
		}
		if !isDestLevel {
			brokerByName[dp.Name] = dp.Value
		}
	}

	if got := brokerByName["senhub.activemq.up"]; got != 1 {
		t.Errorf("senhub.activemq.up = %v, want 1", got)
	}
	if got := brokerByName["activemq.producer.count"]; got != 3 {
		t.Errorf("activemq.producer.count (broker) = %v, want 3", got)
	}
	if got := brokerByName["activemq.consumer.count"]; got != 7 {
		t.Errorf("activemq.consumer.count (broker) = %v, want 7", got)
	}
	if got := brokerByName["activemq.message.current"]; got != 42 {
		t.Errorf("activemq.message.current = %v, want 42", got)
	}

	// Memory usage: 20% → 0.20
	if got := brokerByName["activemq.memory.usage"]; got < 0.199 || got > 0.201 {
		t.Errorf("activemq.memory.usage = %v, want ~0.20", got)
	}
}

func TestCollect_BrokerDown_EmitsUpZero(t *testing.T) {
	// Point the probe at a server that refuses connections.
	p := newTestProbe(t, map[string]interface{}{
		"jolokia_url": "http://127.0.0.1:1", // nothing listening
		"username":    "",
		"timeout":     1,
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect must not return an error on broker failure: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least one datapoint even when broker is down")
	}

	byName := map[string]float32{}
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}
	if got := byName["senhub.activemq.up"]; got != 0 {
		t.Errorf("senhub.activemq.up = %v, want 0 when broker is down", got)
	}
}

func TestCollect_QueueMetrics(t *testing.T) {
	srv := jolokiaServer(t)
	defer srv.Close()

	p := newTestProbe(t, map[string]interface{}{
		"jolokia_url": srv.URL + "/api/jolokia",
		"username":    "",
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Two queues × 5 metrics each = 10 per-destination points.
	destCount := 0
	for _, dp := range points {
		for _, tg := range dp.Tags {
			if tg.Key == "destination" {
				destCount++
				break
			}
		}
	}
	if destCount < 10 {
		t.Errorf("expected >= 10 per-destination datapoints, got %d", destCount)
	}
}

func TestMatchesFilter(t *testing.T) {
	p := &activemqProbe{cfg: probeConfig{QueueFilter: []string{"order*", "ship*"}}}

	cases := []struct {
		name string
		want bool
	}{
		{"orders", true},
		{"shipping", true},
		{"dead.letter", false},
	}
	for _, c := range cases {
		if got := p.matchesFilter(c.name); got != c.want {
			t.Errorf("matchesFilter(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMatchesFilter_Empty(t *testing.T) {
	p := &activemqProbe{cfg: probeConfig{}}
	if !p.matchesFilter("any.queue") {
		t.Error("empty filter should pass all names")
	}
}

func TestProbeType(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	if got := p.GetProbeType(); got != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", got, ProbeType)
	}
}

func TestProbeIntervalDefault(t *testing.T) {
	p := newTestProbe(t, map[string]interface{}{})
	if got := p.GetInterval(); got != defaultInterval {
		t.Errorf("GetInterval() = %v, want %v", got, defaultInterval)
	}
}
