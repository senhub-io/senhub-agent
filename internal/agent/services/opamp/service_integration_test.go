package opamp

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server"
	servertypes "github.com/open-telemetry/opamp-go/server/types"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	agentlogger "senhub-agent.go/internal/agent/services/logger"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestLogger(buf *syncBuffer) *agentlogger.Logger {
	logger := zerolog.New(buf).Level(zerolog.DebugLevel)
	return &logger
}

type fakeConfig struct {
	mu        sync.Mutex
	opamp     *configuration.OpAMPConfig
	key       string
	path      string
	callbacks []func(string)
}

func newFakeConfig(key, path string) *fakeConfig {
	return &fakeConfig{key: key, path: path}
}

func (f *fakeConfig) GetOpAMPConfig() *configuration.OpAMPConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opamp == nil {
		return nil
	}
	c := *f.opamp
	return &c
}

func (f *fakeConfig) GetConfigPath() string {
	return f.path
}

func (f *fakeConfig) GetAuthenticationKey() string {
	return f.key
}

func (f *fakeConfig) GetConfiguration() configuration.ConfigurationData {
	return configuration.ConfigurationData{
		Agent: configuration.AgentConfig{AuthenticationKey: f.key},
	}
}

func (f *fakeConfig) OnConfigChanged(cb func(string)) {
	f.mu.Lock()
	f.callbacks = append(f.callbacks, cb)
	f.mu.Unlock()
}

func (f *fakeConfig) setOpAMP(c *configuration.OpAMPConfig) {
	f.mu.Lock()
	f.opamp = c
	cbs := append([]func(string){}, f.callbacks...)
	f.mu.Unlock()

	for _, cb := range cbs {
		cb(f.path)
	}
}

type testServer struct {
	mu                sync.Mutex
	messages          []*protobufs.AgentToServer
	authHeaders       []string
	offerRemoteConfig atomic.Bool

	addr      string
	srv       server.OpAMPServer
	hs        *httptest.Server
	ln        net.Listener
	closeOnce sync.Once
	conns     map[net.Conn]struct{}
	closeErrs []error
}

func (ts *testServer) url(scheme string) string {
	return scheme + "://" + ts.addr + "/v1/opamp"
}

func (ts *testServer) close() {
	if ts.hs == nil {
		return
	}
	ts.closeOnce.Do(func() {
		// A WebSocket connection is hijacked from net/http, so neither
		// CloseClientConnections nor Close reaches it: close it here or
		// the client never learns the server went away.
		ts.mu.Lock()
		for c := range ts.conns {
			if err := c.Close(); err != nil {
				ts.closeErrs = append(ts.closeErrs, err)
			}
		}
		ts.mu.Unlock()
		ts.hs.CloseClientConnections()
		ts.hs.Close()
	})
}

func (ts *testServer) messagesCopy() []*protobufs.AgentToServer {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]*protobufs.AgentToServer, len(ts.messages))
	copy(out, ts.messages)
	return out
}

func (ts *testServer) authHeadersCopy() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]string, len(ts.authHeaders))
	copy(out, ts.authHeaders)
	return out
}

func newTestServerWithAddr(t *testing.T, addr string) *testServer {
	t.Helper()

	var ln net.Listener
	var err error
	if addr == "" {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
	} else {
		var listenErr error
		eventually(t, 5*time.Second, func() bool {
			ln, listenErr = net.Listen("tcp", addr)
			return listenErr == nil
		}, "listen on "+addr)
	}

	ts := &testServer{addr: ln.Addr().String(), ln: ln, conns: map[net.Conn]struct{}{}}
	ts.srv = server.New(nil)

	callbacks := servertypes.Callbacks{
		OnConnecting: func(r *http.Request) servertypes.ConnectionResponse {
			ts.mu.Lock()
			ts.authHeaders = append(ts.authHeaders, r.Header.Get("Authorization"))
			ts.mu.Unlock()

			return servertypes.ConnectionResponse{
				Accept: true,
				ConnectionCallbacks: servertypes.ConnectionCallbacks{
					OnMessage: func(_ context.Context, _ servertypes.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
						ts.mu.Lock()
						clone := proto.Clone(msg).(*protobufs.AgentToServer)
						ts.messages = append(ts.messages, clone)
						ts.mu.Unlock()

						resp := &protobufs.ServerToAgent{InstanceUid: msg.InstanceUid}
						if ts.offerRemoteConfig.Load() {
							resp.RemoteConfig = &protobufs.AgentRemoteConfig{
								Config: &protobufs.AgentConfigMap{
									ConfigMap: map[string]*protobufs.AgentConfigObject{
										"x.yaml": {
											Body:        []byte("a: 1"),
											ContentType: "text/yaml",
										},
									},
								},
								ConfigHash: []byte("h1"),
							}
						}
						return resp
					},
				},
			}
		},
	}

	handler, connContext, err := ts.srv.Attach(server.Settings{Callbacks: callbacks})
	if err != nil {
		if closeErr := ln.Close(); closeErr != nil {
			t.Logf("closing the listener: %v", closeErr)
		}
		t.Fatalf("attach opamp server: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/opamp", handler)
	hs := httptest.NewUnstartedServer(mux)
	hs.Listener = ln
	hs.Config.ConnContext = connContext
	hs.Config.ConnState = func(c net.Conn, state http.ConnState) {
		ts.mu.Lock()
		defer ts.mu.Unlock()
		switch state {
		case http.StateNew, http.StateHijacked:
			ts.conns[c] = struct{}{}
		case http.StateClosed:
			delete(ts.conns, c)
		}
	}
	hs.Start()
	ts.hs = hs

	t.Cleanup(ts.close)
	return ts
}

func eventually(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s: %s", timeout, msg)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setupConfigTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent.yaml"), "config_version: 3\nagent:\n  key: integ-agent-key\n")
	writeFile(t, filepath.Join(dir, "probes.d", "01-p.yaml"), "- name: p1\n  type: cpu\n  params:\n    password: InlineSecretPw\n    api_key: ${secret:integ.apikey}\n")

	provider := secret.NewMemoryProvider()
	if err := provider.Set("integ.apikey", secret.New("StoreSecretValue")); err != nil {
		t.Fatal(err)
	}
	secret.SetProvider(provider)
	t.Cleanup(func() { secret.SetProvider(nil) })

	return filepath.Join(dir, "agent.yaml")
}

func newOpAMPService(t *testing.T, cfg *fakeConfig, buf *syncBuffer) *Service {
	t.Helper()
	start := time.Now()
	return New(Options{
		Logger:    newTestLogger(buf),
		Config:    cfg,
		Version:   "9.9.9",
		StartTime: start,
		Edition:   "oss",
		HostAttributes: func() (map[string]string, error) {
			return map[string]string{
				"host.id":        "host-integ-1",
				"host.name":      "integ-host",
				"os.type":        "linux",
				"os.description": "Integ OS 1",
				"host.arch":      "amd64",
			}, nil
		},
		LicenseTier: func() string { return "pro" },
		HealthInput: func() healthInput {
			return healthInput{
				Probes: []probeInput{{
					Name:    "p1",
					Enabled: true,
					State: agentstate.ProbeRunState{
						Running:   true,
						Health:    "failed",
						LastError: "boom alid_a_INTEGTOKEN",
					},
				}},
				Outputs:   []outputInput{{Name: "otlp"}},
				StartTime: start,
				Now:       time.Now(),
			}
		},
		HealthInterval: 50 * time.Millisecond,
	})
}

func startOpAMPService(t *testing.T, svc *Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := svc.Shutdown(shutdownCtx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
}

func assertKeyValue(t *testing.T, kv *protobufs.KeyValue, key, value string) {
	t.Helper()
	if kv.GetKey() != key {
		t.Errorf("attribute key = %q, want %q", kv.GetKey(), key)
	}
	if kv.GetValue().GetStringValue() != value {
		t.Errorf("attribute %q value = %q, want %q", key, kv.GetValue().GetStringValue(), value)
	}
}

func assertAgentDescription(t *testing.T, desc *protobufs.AgentDescription, wantInstance string) {
	t.Helper()

	expectedID := []struct{ key, value string }{
		{"service.name", ServiceName},
		{"service.instance.id", wantInstance},
		{"service.version", "9.9.9"},
	}

	idAttrs := desc.GetIdentifyingAttributes()
	if len(idAttrs) != len(expectedID) {
		t.Errorf("identifying attributes count = %d, want %d", len(idAttrs), len(expectedID))
	}
	for i, expected := range expectedID {
		if i >= len(idAttrs) {
			break
		}
		assertKeyValue(t, idAttrs[i], expected.key, expected.value)
	}

	for _, kv := range idAttrs {
		if strings.Contains(kv.GetKey(), "subject") {
			t.Errorf("identifying attribute key %q contains \"subject\"", kv.GetKey())
		}
	}

	nonID := desc.GetNonIdentifyingAttributes()
	nonIDMap := make(map[string]string, len(nonID))
	for _, kv := range nonID {
		if strings.Contains(kv.GetKey(), "subject") {
			t.Errorf("non-identifying attribute key %q contains \"subject\"", kv.GetKey())
		}
		nonIDMap[kv.GetKey()] = kv.GetValue().GetStringValue()
	}

	expectedNonID := map[string]string{
		"host.id":                    "host-integ-1",
		"host.name":                  "integ-host",
		"os.type":                    "linux",
		"os.description":             "Integ OS 1",
		"host.arch":                  "amd64",
		"senhub.agent.edition":       "oss",
		"senhub.license.tier":        "pro",
		"senhub.agent.config.source": ConfigSourceLocal,
	}
	for key, value := range expectedNonID {
		if nonIDMap[key] != value {
			t.Errorf("non-identifying attribute %q = %q, want %q", key, nonIDMap[key], value)
		}
	}
}

func effectiveConfigOK(ec *protobufs.EffectiveConfig) bool {
	if ec == nil || ec.GetConfigMap() == nil {
		return false
	}
	configMap := ec.GetConfigMap().GetConfigMap()
	agentCfg, agentOK := configMap["agent.yaml"]
	probesCfg, probesOK := configMap["probes.d/01-p.yaml"]
	if !agentOK || !probesOK {
		return false
	}
	if agentCfg.GetContentType() != "text/yaml" || probesCfg.GetContentType() != "text/yaml" {
		return false
	}
	for _, obj := range configMap {
		body := string(obj.GetBody())
		for _, forbidden := range []string{"InlineSecretPw", "StoreSecretValue", "integ-agent-key"} {
			if strings.Contains(body, forbidden) {
				return false
			}
		}
	}
	return strings.Contains(string(probesCfg.GetBody()), "***")
}

func TestOpAMPReportsToARealServer(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		scheme    string
	}{
		{name: "websocket", transport: configuration.OpAMPTransportWebSocket, scheme: "ws"},
		{name: "http", transport: configuration.OpAMPTransportHTTP, scheme: "http"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runOpAMPRealServerTest(t, tc.transport, tc.scheme)
		})
	}
}

func runOpAMPRealServerTest(t *testing.T, transport, scheme string) {
	t.Helper()
	agentstate.ResetOpAMPForTest()
	t.Cleanup(agentstate.ResetOpAMPForTest)

	buf := &syncBuffer{}
	configPath := setupConfigTree(t)
	cfg := newFakeConfig("integ-agent-key", configPath)

	srv := newTestServerWithAddr(t, "")
	cfg.setOpAMP(&configuration.OpAMPConfig{
		Enabled:           true,
		Endpoint:          srv.url(scheme),
		Token:             "alid_a_INTEGTOKEN",
		Transport:         transport,
		HeartbeatInterval: "1s",
	})

	svc := newOpAMPService(t, cfg, buf)
	startOpAMPService(t, svc)

	token := "alid_a_INTEGTOKEN"
	wantInstance := configuration.AgentInstanceID("integ-agent-key")
	wantUID := uuid.MustParse(wantInstance)

	eventually(t, 10*time.Second, func() bool {
		for _, msg := range srv.messagesCopy() {
			if msg.GetAgentDescription() != nil {
				return true
			}
		}
		return false
	}, "a message with AgentDescription should arrive")

	messages := srv.messagesCopy()
	var lastDesc *protobufs.AgentDescription
	for _, msg := range messages {
		if msg.GetAgentDescription() != nil {
			lastDesc = msg.GetAgentDescription()
		}
	}
	if lastDesc == nil {
		t.Fatal("no AgentDescription found")
	}
	assertAgentDescription(t, lastDesc, wantInstance)

	for i, msg := range messages {
		if !bytes.Equal(msg.GetInstanceUid(), wantUID[:]) {
			t.Errorf("message %d InstanceUid = %x, want %x", i, msg.GetInstanceUid(), wantUID[:])
		}
	}

	headers := srv.authHeadersCopy()
	if len(headers) == 0 {
		t.Fatal("no Authorization header recorded")
	}
	for i, header := range headers {
		if header != "Bearer "+token {
			t.Errorf("Authorization header %d = %q, want %q", i, header, "Bearer "+token)
		}
	}

	requiredCaps := uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus) |
		uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig) |
		uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsHealth) |
		uint64(protobufs.AgentCapabilities_AgentCapabilities_ReportsHeartbeat)
	forbiddenCaps := uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig)

	eventually(t, 10*time.Second, func() bool {
		for _, msg := range srv.messagesCopy() {
			caps := msg.GetCapabilities()
			if caps&requiredCaps == requiredCaps && caps&forbiddenCaps == 0 {
				return true
			}
		}
		return false
	}, "capabilities should be reported")

	eventually(t, 10*time.Second, func() bool {
		for _, msg := range srv.messagesCopy() {
			health := msg.GetHealth()
			if health == nil {
				continue
			}
			if health.GetHealthy() || health.GetStatus() != "degraded" {
				continue
			}
			components := health.GetComponentHealthMap()
			probe := components["probe:p1"]
			output := components["output:otlp"]
			if probe == nil || probe.GetHealthy() || probe.GetStatus() != "failed" {
				continue
			}
			if strings.Contains(probe.GetLastError(), token) {
				continue
			}
			if output == nil || !output.GetHealthy() {
				continue
			}
			return true
		}
		return false
	}, "health should be reported")

	eventually(t, 10*time.Second, func() bool {
		for _, msg := range srv.messagesCopy() {
			if effectiveConfigOK(msg.GetEffectiveConfig()) {
				return true
			}
		}
		return false
	}, "effective config should be reported")

	eventually(t, 10*time.Second, func() bool {
		return svc.Connected() && agentstate.GetOpAMPState().Connected
	}, "service should report connected")

	before := len(srv.messagesCopy())
	srv.offerRemoteConfig.Store(true)

	eventually(t, 10*time.Second, func() bool {
		return len(srv.messagesCopy()) >= before+2
	}, "at least two more heartbeats should arrive after offering remote config")

	if got := strings.Count(buf.String(), "does not accept remote configuration"); got != 1 {
		t.Errorf("remote config ignored log count = %d, want 1", got)
	}
	if !svc.Connected() || !agentstate.GetOpAMPState().Connected {
		t.Errorf("service should still be connected after ignoring remote config")
	}

	addr := srv.addr
	srv.close()

	eventually(t, 15*time.Second, func() bool {
		return !agentstate.GetOpAMPState().Connected
	}, "connected should become false after the server is closed")

	srv2 := newTestServerWithAddr(t, addr)
	eventually(t, 30*time.Second, func() bool {
		for _, msg := range srv2.messagesCopy() {
			if bytes.Equal(msg.GetInstanceUid(), wantUID[:]) {
				return true
			}
		}
		return false
	}, "new server should receive a message from this agent")

	eventually(t, 30*time.Second, func() bool {
		return svc.Connected() && agentstate.GetOpAMPState().Connected
	}, "service should reconnect to the new server")

	if strings.Contains(buf.String(), token) {
		t.Errorf("logs must not contain the OpAMP token")
	}
}

func TestOpAMPFollowsConfigurationChanges(t *testing.T) {
	agentstate.ResetOpAMPForTest()
	t.Cleanup(agentstate.ResetOpAMPForTest)

	buf := &syncBuffer{}
	configPath := setupConfigTree(t)
	cfg := newFakeConfig("integ-agent-key", configPath)
	srv := newTestServerWithAddr(t, "")

	base := &configuration.OpAMPConfig{
		Enabled:           true,
		Endpoint:          srv.url("ws"),
		Token:             "alid_a_FIRST",
		Transport:         configuration.OpAMPTransportWebSocket,
		HeartbeatInterval: "1s",
	}
	cfg.setOpAMP(base)

	svc := newOpAMPService(t, cfg, buf)
	startOpAMPService(t, svc)

	eventually(t, 10*time.Second, func() bool {
		for _, header := range srv.authHeadersCopy() {
			if header == "Bearer alid_a_FIRST" {
				return true
			}
		}
		return false
	}, "first token should be used")

	second := *base
	second.Token = "alid_a_SECOND"
	cfg.setOpAMP(&second)

	eventually(t, 10*time.Second, func() bool {
		for _, header := range srv.authHeadersCopy() {
			if header == "Bearer alid_a_SECOND" {
				return true
			}
		}
		return false
	}, "second token should be used after reconfiguration")

	cfg.setOpAMP(&configuration.OpAMPConfig{Enabled: false})

	eventually(t, 10*time.Second, func() bool {
		return !svc.Connected() && !agentstate.GetOpAMPState().Enabled
	}, "service should stop and become disabled")

	logs := buf.String()
	if strings.Contains(logs, "alid_a_FIRST") || strings.Contains(logs, "alid_a_SECOND") {
		t.Errorf("logs must not contain either OpAMP token")
	}
}

func TestOpAMPInvalidBlockStartsNothing(t *testing.T) {
	agentstate.ResetOpAMPForTest()
	t.Cleanup(agentstate.ResetOpAMPForTest)

	buf := &syncBuffer{}
	configPath := setupConfigTree(t)
	cfg := newFakeConfig("integ-agent-key", configPath)
	srv := newTestServerWithAddr(t, "")

	cfg.setOpAMP(&configuration.OpAMPConfig{
		Enabled:           true,
		Endpoint:          srv.url("ws"),
		Token:             "",
		Transport:         configuration.OpAMPTransportWebSocket,
		HeartbeatInterval: "1s",
	})

	svc := newOpAMPService(t, cfg, buf)
	startOpAMPService(t, svc)

	if svc.Connected() {
		t.Error("service must not be connected for an invalid block")
	}
	if agentstate.GetOpAMPState().Enabled {
		t.Error("OpAMP state must be disabled for an invalid block")
	}
	if !strings.Contains(buf.String(), "configuration is invalid") {
		t.Errorf("log should contain configuration invalid message; got: %s", buf.String())
	}

	start := time.Now()
	eventually(t, 1*time.Second, func() bool {
		return time.Since(start) >= 300*time.Millisecond &&
			len(srv.messagesCopy()) == 0 &&
			len(srv.authHeadersCopy()) == 0
	}, "server should receive no connection within 300ms")
}

func TestOpAMPShutdownWithoutStart(t *testing.T) {
	agentstate.ResetOpAMPForTest()
	t.Cleanup(agentstate.ResetOpAMPForTest)

	buf := &syncBuffer{}
	svc := New(Options{Logger: newTestLogger(buf)})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown before Start returned error: %v", err)
	}
}
