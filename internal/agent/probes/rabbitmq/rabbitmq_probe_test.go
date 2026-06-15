package rabbitmq

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

// sampleOverview is a minimal /api/overview JSON response.
const sampleOverview = `{
	"message_stats": {
		"publish": 1000,
		"deliver_get": 900,
		"ack": 850
	},
	"queue_totals": {
		"messages_unacknowledged": 50,
		"messages_ready": 100
	},
	"object_totals": {
		"consumers": 5,
		"queues": 3,
		"connections": 10,
		"channels": 20
	}
}`

// sampleNodes is a minimal /api/nodes JSON response.
const sampleNodes = `[
	{
		"name": "rabbit@myhost",
		"mem_used": 104857600,
		"disk_free": 10737418240,
		"fd_used": 42,
		"sockets_used": 10,
		"running": true,
		"uptime": 3600000
	}
]`

// sampleQueues is a minimal /api/queues JSON response.
const sampleQueues = `[
	{
		"name": "my.queue",
		"vhost": "/",
		"messages_ready": 7,
		"messages_unacknowledged": 2,
		"consumers": 1
	}
]`

func newFakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "guest" || pass != "guest" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleOverview))
	})
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleNodes))
	})
	mux.HandleFunc("/api/queues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleQueues))
	})
	return httptest.NewServer(mux)
}

func TestNewRabbitMQProbe_Defaults(t *testing.T) {
	probe, err := NewRabbitMQProbe(map[string]interface{}{}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe() error = %v", err)
	}
	if probe == nil {
		t.Fatal("NewRabbitMQProbe() returned nil probe")
	}
	p := probe.(*rabbitProbe)
	if p.GetProbeType() != ProbeType {
		t.Errorf("GetProbeType() = %q, want %q", p.GetProbeType(), ProbeType)
	}
	if !probe.ShouldStart() {
		t.Error("ShouldStart() should return true")
	}
	if probe.GetInterval() != defaultInterval {
		t.Errorf("GetInterval() = %v, want %v", probe.GetInterval(), defaultInterval)
	}
}

func TestNewRabbitMQProbe_CustomConfig(t *testing.T) {
	probe, err := NewRabbitMQProbe(map[string]interface{}{
		"endpoint": "http://rabbitmq.example.com:15672",
		"username": "admin",
		"password": "secret",
		"interval": 30,
		"timeout":  5,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe() error = %v", err)
	}
	p := probe.(*rabbitProbe)
	if p.cfg.Endpoint != "http://rabbitmq.example.com:15672" {
		t.Errorf("Endpoint = %q", p.cfg.Endpoint)
	}
	if p.cfg.Username != "admin" {
		t.Errorf("Username = %q", p.cfg.Username)
	}
	if p.cfg.Interval.Seconds() != 30 {
		t.Errorf("Interval = %v, want 30s", p.cfg.Interval)
	}
}

func TestCollect_HappyPath(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	probe, err := NewRabbitMQProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "guest",
		"password": "guest",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() returned unexpected error: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints")
	}

	// Index by metric name for assertion.
	byName := make(map[string]float64)
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	cases := []struct {
		name string
		want float64
	}{
		{"senhub.rabbitmq.up", 1},
		{"rabbitmq.messages.published", 1000},
		{"rabbitmq.messages.delivered", 900},
		{"rabbitmq.messages.acknowledged", 850},
		{"rabbitmq.messages.unacknowledged", 50},
		{"rabbitmq.messages.ready", 100},
		{"rabbitmq.consumers.total", 5},
		{"rabbitmq.queues.total", 3},
		{"rabbitmq.connections.total", 10},
		{"rabbitmq.channels.total", 20},
		{"rabbitmq.node.memory.used", 104857600},
		{"rabbitmq.node.disk.free", 10737418240},
		{"rabbitmq.node.fd.used", 42},
		{"rabbitmq.node.sockets.used", 10},
		{"rabbitmq.node.running", 1},
		{"rabbitmq.node.uptime", 3600000},
		{"rabbitmq.queue.messages.ready", 7},
		{"rabbitmq.queue.messages.unacknowledged", 2},
		{"rabbitmq.queue.consumers", 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := byName[tc.name]
			if !ok {
				t.Errorf("metric %q not found in output", tc.name)
				return
			}
			if got != tc.want {
				t.Errorf("metric %q = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestCollect_Unreachable(t *testing.T) {
	probe, err := NewRabbitMQProbe(map[string]interface{}{
		"endpoint": "http://127.0.0.1:1", // nothing listening
		"timeout":  1,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() should not return an error on unreachable target, got: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.rabbitmq.up" {
			if dp.Value != 0 {
				t.Errorf("senhub.rabbitmq.up = %v, want 0 when unreachable", dp.Value)
			}
			return
		}
	}
	t.Error("senhub.rabbitmq.up not found in output")
}

func TestCollect_BadAuth(t *testing.T) {
	srv := newFakeServer(t)
	defer srv.Close()

	probe, err := NewRabbitMQProbe(map[string]interface{}{
		"endpoint": srv.URL,
		"username": "wrong",
		"password": "wrong",
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() should not error on auth failure, got: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "senhub.rabbitmq.up" {
			if dp.Value != 0 {
				t.Errorf("senhub.rabbitmq.up = %v, want 0 on auth failure", dp.Value)
			}
			return
		}
	}
	t.Error("senhub.rabbitmq.up not found in output")
}

func TestCollect_MissingFields(t *testing.T) {
	// Simulate a broker with no traffic: message_stats fields absent.
	const sparseOverview = `{"queue_totals":{},"object_totals":{"queues":1}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/overview":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sparseOverview))
		default:
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()

	probe, err := NewRabbitMQProbe(map[string]interface{}{
		"endpoint": srv.URL,
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	byName := make(map[string]float64)
	for _, dp := range points {
		byName[dp.Name] = dp.Value
	}

	if byName["senhub.rabbitmq.up"] != 1 {
		t.Errorf("senhub.rabbitmq.up = %v, want 1", byName["senhub.rabbitmq.up"])
	}
	if byName["rabbitmq.queues.total"] != 1 {
		t.Errorf("rabbitmq.queues.total = %v, want 1", byName["rabbitmq.queues.total"])
	}
	if _, ok := byName["rabbitmq.messages.published"]; ok {
		t.Error("rabbitmq.messages.published should not appear when field is absent")
	}
}

func TestNodeRunning_FalseValue(t *testing.T) {
	nodesDown := `[{"name":"rabbit@down","running":false}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/overview":
			_, _ = w.Write([]byte(`{"queue_totals":{},"object_totals":{},"message_stats":{}}`))
		case "/api/nodes":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(nodesDown))
		default:
			_, _ = w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()

	probe, err := NewRabbitMQProbe(map[string]interface{}{"endpoint": srv.URL}, newTestLogger())
	if err != nil {
		t.Fatalf("NewRabbitMQProbe: %v", err)
	}

	points, err := probe.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	for _, dp := range points {
		if dp.Name == "rabbitmq.node.running" {
			if dp.Value != 0 {
				t.Errorf("rabbitmq.node.running = %v, want 0 for stopped node", dp.Value)
			}
			return
		}
	}
	t.Error("rabbitmq.node.running metric not found")
}

func TestFetchJSON_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	p := &rabbitProbe{
		cfg:    rabbitConfig{Endpoint: srv.URL},
		client: &http.Client{},
	}

	var dst json.RawMessage
	err := p.fetchJSON("/api/overview", &dst)
	// should succeed — raw JSON is valid as []byte; but here we pass a typed struct
	// to trigger the real path. Use a typed target to exercise the unmarshal error.
	type strict struct {
		Field int `json:"field"`
	}
	var s strict
	err = p.fetchJSON("/api/overview", &s)
	// json.Unmarshal("not-json") will fail
	if err == nil {
		t.Error("fetchJSON should fail on invalid JSON body")
	}
	_ = err
}
