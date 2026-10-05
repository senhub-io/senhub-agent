package statuschan

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

// shortDir keeps the socket path under the 104-byte sun_path limit that
// t.TempDir() can exceed on macOS.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startTestService(t *testing.T, dir string) *Service {
	t.Helper()
	svc := New(logger.NewLogger(&cliArgs.ParsedArgs{}), filepath.Join(dir, "agent.yaml"), "9.9.9", "abcdef123456")
	if runtime.GOOS != "windows" {
		svc.paths = []string{socketIn(dir)}
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	return svc
}

func TestQueryReturnsTheAgentStatus(t *testing.T) {
	dir := shortDir(t)
	svc := startTestService(t, dir)

	st, err := queryPaths(svc.paths)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if st.Agent.Version != "9.9.9" || st.Health.Status == "" {
		t.Errorf("status not the agent's own: %+v", st.Agent)
	}
	if st.Performance.Uptime == "" {
		t.Error("the performance block is part of the served status")
	}
}

func TestSnapshotCarriesProbeStatesAndStrategyFailures(t *testing.T) {
	agentstate.SetActiveProbes([]string{"id-a", "id-b"})
	agentstate.SetActiveProbeNames(map[string]string{"alpha": "id-a", "beta": "id-b"})
	agentstate.RecordProbeHealth("id-a", true)
	agentstate.RecordProbeHealth("id-b", false)
	agentstate.RecordProbeError("id-b", "boom")
	agentstate.RecordStrategyFailure("otlp", "bind", "port taken")
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
		agentstate.SetActiveProbeNames(nil)
		agentstate.ResetStrategyFailuresForTest()
	})

	st := Snapshot(status.NewStatusService(logger.NewLogger(&cliArgs.ParsedArgs{}), "1", "c"))
	if len(st.Probes) != 2 || st.Probes[0].Name != "alpha" || st.Probes[0].Status != "active" || st.Probes[1].Status != "error" || st.Probes[1].LastError != "boom" {
		t.Errorf("probes = %+v", st.Probes)
	}
	if st.Health.Status == "healthy" {
		t.Errorf("a failing probe must show in health, got %+v", st.Health)
	}
	if len(st.StrategyFailures) != 1 || st.StrategyFailures[0].Strategy != "otlp" {
		t.Errorf("strategy failures = %+v", st.StrategyFailures)
	}
}

func TestNothingListeningIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pipe name is fixed")
	}
	if _, err := queryPaths([]string{socketIn(shortDir(t))}); err == nil {
		t.Fatal("no agent behind the socket must be an error so the caller falls back")
	}
}

func TestShutdownRemovesTheSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes leave no file")
	}
	dir := shortDir(t)
	svc := startTestService(t, dir)
	if _, err := os.Lstat(socketIn(dir)); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketIn(dir)); !os.IsNotExist(err) {
		t.Errorf("socket must be removed on shutdown, err=%v", err)
	}
}

func TestSocketIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the pipe carries an ACL, not a mode")
	}
	dir := shortDir(t)
	startTestService(t, dir)
	fi, err := os.Lstat(socketIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %o, want 600", perm)
	}
}

func TestStaleSocketIsReplacedButAForeignFileIsNot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket files only")
	}
	dir := shortDir(t)
	// A socket left by a killed agent: bound, then abandoned.
	old, err := net.Listen("unix", socketIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = old.Close()
	svc := startTestService(t, dir)
	if _, err := queryPaths(svc.paths); err != nil {
		t.Fatalf("a stale socket must not block the channel: %v", err)
	}

	other := shortDir(t)
	if err := os.WriteFile(socketIn(other), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ln, _, err := listen([]string{socketIn(other)}); err == nil {
		_ = ln.Close()
		t.Fatal("a regular file at the socket path must not be deleted")
	}
	if b, _ := os.ReadFile(socketIn(other)); string(b) != "data" {
		t.Error("the foreign file was modified")
	}
}

func TestClientInputIsNeverRead(t *testing.T) {
	dir := shortDir(t)
	svc := startTestService(t, dir)
	conn, err := dial(svc.paths[0], time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(`{"command":"stop"}` + "\n"))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("the agent answers regardless of what was sent: %v", err)
	}
}

func TestCandidatePathsOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("one fixed pipe")
	}
	t.Setenv("STATE_DIRECTORY", "/run/state:/other")
	got := candidatePaths("/etc/x/agent.yaml")
	if len(got) < 2 || got[0] != "/run/state/control.sock" || got[len(got)-1] != "/etc/x/control.sock" {
		t.Errorf("paths = %v: state directory first, config directory last", got)
	}
}
