package swarm

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestClusterServiceInstanceID_EmptyAndPrefixed(t *testing.T) {
	cases := []struct {
		name      string
		clusterID string
		want      string
	}{
		{name: "empty id", clusterID: "", want: ""},
		{name: "swarm id is prefixed", clusterID: "cluster-xyz", want: "swarm://cluster-xyz"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterServiceInstanceID(tt.clusterID); got != tt.want {
				t.Errorf("clusterServiceInstanceID(%q) = %q, want %q", tt.clusterID, got, tt.want)
			}
		})
	}
}

func TestEntitySource_ServiceInstanceID_TracksUpdate(t *testing.T) {
	src := newEntitySource()

	if got := src.serviceInstanceID(); got != "" {
		t.Fatalf("new entity source reports id %q, want nothing pinned", got)
	}

	src.update("cluster-1", "default", 2, 1, nil)
	if got, want := src.serviceInstanceID(), "swarm://cluster-1"; got != want {
		t.Errorf("after update serviceInstanceID() = %q, want %q", got, want)
	}

	src.update("", "", 0, 0, nil)
	if got := src.serviceInstanceID(); got != "" {
		t.Errorf("after withdrawal serviceInstanceID() = %q, want nothing pinned", got)
	}
}

func TestHasPublishedPort_ChecksBothEndpointSlots(t *testing.T) {
	cases := []struct {
		name string
		s    *service
		want bool
	}{
		{
			name: "no ports",
			s:    &service{},
			want: false,
		},
		{
			name: "endpoint port with no published port",
			s: func() *service {
				s := &service{}
				s.Endpoint.Ports = []portConfig{{TargetPort: 80}}
				return s
			}(),
			want: false,
		},
		{
			name: "endpoint published port",
			s: func() *service {
				s := &service{}
				s.Endpoint.Ports = []portConfig{{PublishedPort: 8080, TargetPort: 80}}
				return s
			}(),
			want: true,
		},
		{
			name: "spec published port",
			s: func() *service {
				s := &service{}
				s.Endpoint.Spec.Ports = []portConfig{{PublishedPort: 8443, TargetPort: 443}}
				return s
			}(),
			want: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPublishedPort(tt.s); got != tt.want {
				t.Errorf("hasPublishedPort() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrimarySubnet_ReturnsFirstIPAMConfigOrEmpty(t *testing.T) {
	cases := []struct {
		name string
		n    *network
		want string
	}{
		{name: "no ipam config", n: &network{}, want: ""},
		{
			name: "first config is chosen",
			n: &network{IPAM: struct {
				Config []struct {
					Subnet  string `json:"Subnet"`
					Gateway string `json:"Gateway"`
				} `json:"Config"`
			}{
				Config: []struct {
					Subnet  string `json:"Subnet"`
					Gateway string `json:"Gateway"`
				}{
					{Subnet: "10.0.1.0/24", Gateway: "10.0.1.1"},
					{Subnet: "10.0.2.0/24", Gateway: "10.0.2.1"},
				},
			}},
			want: "10.0.1.0/24",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := primarySubnet(tt.n); got != tt.want {
				t.Errorf("primarySubnet() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublishedPortPoints_EmitsOnlyPublishedPortsFromSpecOrEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		setup    func() *service
		present  bool
		wantTags map[string]string
	}{
		{
			name: "endpoint port with ingress default",
			setup: func() *service {
				s := &service{}
				s.Endpoint.Ports = []portConfig{{PublishedPort: 8080, TargetPort: 80, Protocol: "TCP"}}
				return s
			},
			present: true,
			wantTags: map[string]string{
				"swarm.port.published": "8080",
				"swarm.port.target":    "80",
				"network.transport":    "tcp",
				"swarm.port.mode":      "ingress",
			},
		},
		{
			name: "spec port with uppercase mode and protocol",
			setup: func() *service {
				s := &service{}
				s.Endpoint.Spec.Ports = []portConfig{{PublishedPort: 9000, TargetPort: 8080, Protocol: "UDP", PublishMode: "HOST"}}
				return s
			},
			present: true,
			wantTags: map[string]string{
				"swarm.port.published": "9000",
				"swarm.port.target":    "8080",
				"network.transport":    "udp",
				"swarm.port.mode":      "host",
			},
		},
		{
			name: "port without published port is skipped",
			setup: func() *service {
				s := &service{}
				s.Endpoint.Ports = []portConfig{{TargetPort: 80, Protocol: "TCP"}}
				return s
			},
			present: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := &swarmProbe{}
			points := p.publishedPortPoints(tt.setup(), nil, time.Now())
			if tt.present {
				if got, ok := value(t, points, "swarm.service.port.published", tt.wantTags); !ok || got != 1 {
					t.Errorf("published port metric with tags %v missing or not 1", tt.wantTags)
				}
				return
			}
			if got, ok := value(t, points, "swarm.service.port.published", nil); ok {
				t.Errorf("a port with PublishedPort 0 emitted metric value %v", got)
			}
		})
	}
}

func TestPublishMode_DefaultsToIngressAndLowercases(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "ingress"},
		{in: "   ", want: "ingress"},
		{in: "INGRESS", want: "ingress"},
		{in: "Host", want: "host"},
	}
	for _, tt := range cases {
		if got := publishMode(tt.in); got != tt.want {
			t.Errorf("publishMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestServiceMode_ReplicatedUnknownAndNilReplicas(t *testing.T) {
	five := int64(5)
	cases := []struct {
		name        string
		setup       func() *service
		wantMode    string
		wantDesired int64
	}{
		{
			name: "replicated with replicas",
			setup: func() *service {
				s := &service{ID: "s1"}
				s.Spec.Mode.Replicated = &struct {
					Replicas *int64 `json:"Replicas"`
				}{Replicas: &five}
				return s
			},
			wantMode:    "replicated",
			wantDesired: 5,
		},
		{
			name: "replicated with nil replicas",
			setup: func() *service {
				s := &service{ID: "s2"}
				s.Spec.Mode.Replicated = &struct {
					Replicas *int64 `json:"Replicas"`
				}{Replicas: nil}
				return s
			},
			wantMode:    "replicated",
			wantDesired: 0,
		},
		{
			name: "unknown mode",
			setup: func() *service {
				return &service{ID: "s3"}
			},
			wantMode:    "unknown",
			wantDesired: 0,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			mode, desired := serviceMode(tt.setup(), nil)
			if mode != tt.wantMode || desired != tt.wantDesired {
				t.Errorf("serviceMode() = (%q, %d), want (%q, %d)", mode, desired, tt.wantMode, tt.wantDesired)
			}
		})
	}
}

func TestHTTPStatusError_FormatsPathAndCode(t *testing.T) {
	err := &httpStatusError{code: http.StatusNotFound, path: "/swarm"}
	if got, want := err.Error(), "GET /swarm returned 404"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestFetchSwarm_OKMalformedAndHTTPError(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": map[string]any{"ID": "c1", "Spec": map[string]any{"Name": "prod"}},
		}))
		info, state, err := p.fetchSwarm()
		if err != nil {
			t.Fatalf("fetchSwarm: %v", err)
		}
		if state != stateManager {
			t.Errorf("state = %v, want manager", state)
		}
		if info.ID != "c1" {
			t.Errorf("info.ID = %q, want c1", info.ID)
		}
	})

	t.Run("non-503 http status", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": errBody{http.StatusNotFound, "not found"},
		}))
		_, state, err := p.fetchSwarm()
		if state != stateUnreachable {
			t.Errorf("state = %v, want unreachable", state)
		}
		if err == nil || !strings.Contains(err.Error(), "GET /swarm returned 404") {
			t.Errorf("err = %v, want GET /swarm returned 404", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": `[`,
		}))
		_, state, err := p.fetchSwarm()
		if state != stateUnreachable {
			t.Errorf("state = %v, want unreachable", state)
		}
		if err == nil || !strings.Contains(err.Error(), "decoding /swarm") {
			t.Errorf("err = %v, want decoding /swarm", err)
		}
	})
}

func TestFetchSwarm_UnavailableBodyFallbackWhenInfoFails(t *testing.T) {
	t.Run("worker nodes body without usable info", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": errBody{http.StatusServiceUnavailable, "This node is not a swarm manager. Worker nodes can't be used to view or modify cluster state."},
			"/info":  errBody{http.StatusInternalServerError, "boom"},
		}))
		_, state, err := p.fetchSwarm()
		if err != nil {
			t.Fatalf("fetchSwarm: %v; 503 is information, not an error", err)
		}
		if state != stateWorker {
			t.Errorf("state = %v, want worker", state)
		}
	})

	t.Run("not-in-swarm body without usable info", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": errBody{http.StatusServiceUnavailable, `This node is not a swarm manager. Use "docker swarm init" to connect.`},
			"/info":  errBody{http.StatusInternalServerError, "boom"},
		}))
		_, state, err := p.fetchSwarm()
		if err != nil {
			t.Fatalf("fetchSwarm: %v; 503 is information, not an error", err)
		}
		if state != stateNotInSwarm {
			t.Errorf("state = %v, want not_in_swarm", state)
		}
	})
}

func TestGet_StatusBodyDialErrorAndBadRequest(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": map[string]any{"ID": "c1"},
		}))
		body, status, err := p.get("/swarm")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if status != http.StatusOK {
			t.Errorf("status = %d, want 200", status)
		}
		if !strings.Contains(string(body), `"ID"`) {
			t.Errorf("body = %q, want the encoded /swarm object", body)
		}
	})

	t.Run("bad request still returns body and status", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/swarm": errBody{http.StatusBadRequest, "bad request"},
		}))
		body, status, err := p.get("/swarm")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", status)
		}
		if string(body) != "bad request" {
			t.Errorf("body = %q, want bad request", body)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Unix sockets unavailable on this runner")
		}
		sock, err := shortSocketPath(t)
		if err != nil {
			t.Fatalf("socket path: %v", err)
		}
		probe, err := NewSwarmProbe(map[string]interface{}{"socket_path": sock, "timeout": 1},
			logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
		if err != nil {
			t.Fatalf("NewSwarmProbe: %v", err)
		}
		p := probe.(*swarmProbe)
		_, status, err := p.get("/swarm")
		if err == nil {
			t.Fatal("get succeeded against a refused Unix socket")
		}
		if status != 0 {
			t.Errorf("status = %d, want 0 on transport error", status)
		}
	})
}

func TestGetJSON_ListStatusAndDecodePaths(t *testing.T) {
	t.Run("decodes list", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/nodes": []node{{ID: "n1"}},
		}))
		got, err := getJSON[node](p, "/nodes")
		if err != nil {
			t.Fatalf("getJSON: %v", err)
		}
		if len(got) != 1 || got[0].ID != "n1" {
			t.Errorf("got %+v, want one node n1", got)
		}
	})

	t.Run("http error carries path and status", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/nodes": errBody{http.StatusUnauthorized, "auth required"},
		}))
		_, err := getJSON[node](p, "/nodes")
		if err == nil || !strings.Contains(err.Error(), "GET /nodes returned 401") {
			t.Errorf("err = %v, want GET /nodes returned 401", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		p := newTestProbe(t, engine(t, map[string]any{
			"/nodes": `[`,
		}))
		_, err := getJSON[node](p, "/nodes")
		if err == nil || !strings.Contains(err.Error(), "decoding /nodes") {
			t.Errorf("err = %v, want decoding /nodes", err)
		}
	})
}

func TestExplainOnce_SetsFlagAndToleratesSubsequentCalls(t *testing.T) {
	p := &swarmProbe{
		moduleLogger: logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}), "probe.swarm.test"),
		cfg:          probeConfig{SocketPath: "/tmp/test.sock"},
	}

	p.explainOnce(stateWorker, nil)
	if !p.notManagerLogged.Load() {
		t.Fatal("explainOnce did not set the once flag on first call")
	}

	// The second call would log again if the guard were missing; this test is
	// the white-box guard on the atomic branch, since the prose goes to stderr.
	p.explainOnce(stateWorker, nil)
	if !p.notManagerLogged.Load() {
		t.Fatal("the once flag was cleared unexpectedly")
	}
}

func TestClassifyCheckError_UsesStatusCodeOrTransportClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want dbcommon.CheckResult
	}{
		{"http 401 wrapped", fmt.Errorf("listing nodes: %w", &httpStatusError{code: http.StatusUnauthorized, path: "/nodes"}), dbcommon.ClassifyHTTPStatus(http.StatusUnauthorized)},
		{"http 500", &httpStatusError{code: http.StatusInternalServerError, path: "/services"}, dbcommon.ClassifyHTTPStatus(http.StatusInternalServerError)},
		{"connection refused text", errors.New("dial unix /var/run/docker.sock: connect: connection refused"), dbcommon.CheckTargetDown},
		{"nil error", nil, dbcommon.CheckOK},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyCheckError(tt.err); got != tt.want {
				t.Errorf("classifyCheckError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCollect_CheckMetricReflectsFailedList(t *testing.T) {
	p := newTestProbe(t, engine(t, map[string]any{
		"/swarm": map[string]any{"ID": "c1"},
		"/nodes": errBody{http.StatusUnauthorized, "auth required"},
	}))

	points, err := p.Collect()
	if err == nil {
		t.Fatal("Collect: expected a failed list to surface as an error")
	}

	check, ok := value(t, points, dbcommon.CheckResultMetric, map[string]string{"metric_type": "status"})
	if !ok {
		t.Fatal("missing check result metric after a failed list")
	}
	if check == float64(dbcommon.CheckOK) {
		t.Errorf("check result = %v, want non-OK after a failed read", check)
	}
}
