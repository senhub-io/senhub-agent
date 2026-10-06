package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/cliArgs"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

type runningService struct{ service.Service }

func (runningService) Status() (service.Status, error) { return service.StatusRunning, nil }

func withStatusChannel(t *testing.T, fn func(string) (*status.SystemStatus, error)) {
	t.Helper()
	prev := askStatusChannel
	askStatusChannel = fn
	t.Cleanup(func() { askStatusChannel = prev })
}

func statusConfig(t *testing.T, httpPort int) *cliArgs.ParsedArgs {
	t.Helper()
	// validateConfigPath only accepts the installed configuration or a
	// file under the working directory.
	dir, err := os.MkdirTemp(".", "stcfg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if dir, err = filepath.Abs(dir); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("config_version: 3\nagent:\n  key: \"k\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("http:\n  port: %d\n  endpoints: [\"prtg\"]\n", httpPort)
	if err := os.WriteFile(filepath.Join(dir, "strategies.d", "00-http.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return &cliArgs.ParsedArgs{ConfigPath: main}
}

func httpStatusServer(t *testing.T, hits *atomic.Int32) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/api/k/info/system":
			_, _ = w.Write([]byte(`{"status":"running","version":"from-http","health":{"status":"healthy"}}`))
		case "/api/k/info/probes":
			_, _ = w.Write([]byte(`{"probes":[],"details":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	return port
}

func quietLogger() *agentLogger.Logger {
	return agentLogger.NewLogger(&cliArgs.ParsedArgs{})
}

func TestStatusAsksTheLocalChannelBeforeHTTP(t *testing.T) {
	var hits atomic.Int32
	args := statusConfig(t, httpStatusServer(t, &hits))
	withStatusChannel(t, func(string) (*status.SystemStatus, error) {
		return &status.SystemStatus{Agent: status.AgentInfo{Version: "from-channel"}, Health: status.HealthInfo{Status: "healthy"}}, nil
	})

	res := collectStatusWith(runningService{}, args, quietLogger())
	if res.source != "daemon" || res.system.Agent.Version != "from-channel" {
		t.Fatalf("the channel must answer first, got source=%s version=%s", res.source, res.system.Agent.Version)
	}
	if hits.Load() != 0 {
		t.Errorf("HTTP was asked %d times although the channel answered", hits.Load())
	}
}

func TestStatusFallsBackToHTTPWhenTheChannelDoesNotAnswer(t *testing.T) {
	var hits atomic.Int32
	args := statusConfig(t, httpStatusServer(t, &hits))
	withStatusChannel(t, func(string) (*status.SystemStatus, error) { return nil, errors.New("no socket") })

	res := collectStatusWith(runningService{}, args, quietLogger())
	if res.source != "daemon" || res.system.Agent.Version != "from-http" {
		t.Fatalf("HTTP must answer when the channel does not, got source=%s version=%s notice=%q", res.source, res.system.Agent.Version, res.notice)
	}
}

func TestStatusKeepsTheDegradedMessageWhenNothingAnswers(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	args := statusConfig(t, port)
	withStatusChannel(t, func(string) (*status.SystemStatus, error) { return nil, errors.New("no socket") })

	res := collectStatusWith(runningService{}, args, quietLogger())
	if res.source == "daemon" {
		t.Fatalf("nothing answered, the source cannot be the daemon: %s", res.source)
	}
	if res.notice == "" {
		t.Error("the degraded notice explaining the daemon did not answer must be kept")
	}
}
