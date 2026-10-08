// Package statuschan is the agent's local, read-only status channel.
//
// `senhub-agent status` used to learn the running agent's state only
// through the HTTP output. A host installed before that output was on by
// default declares no listener, so the command could not ask and fell
// back to a view computed by its own process. This channel answers on a
// Unix socket (a named pipe on Windows) owned by the service account,
// independent of any output: whatever the configuration says, the
// operator can read the agent's state on the machine it runs on.
//
// The protocol has no request. A client connects, the agent writes the
// status as one JSON document and closes. Nothing a client sends is
// read, so the channel cannot be used to change anything.
package statuschan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

const (
	socketName   = "control.sock"
	writeTimeout = 2 * time.Second
	dialTimeout  = 2 * time.Second
	readLimit    = 4 << 20
)

// Service serves the status channel for the lifetime of the agent.
type Service struct {
	logger     *logger.ModuleLogger
	paths      []string
	build      func() status.SystemStatus
	mu         sync.Mutex
	listener   net.Listener
	boundPath  string
	loopExited chan struct{}
}

// New builds the service. configPath locates the configuration file, the
// last-resort place for the socket when the state directory is missing.
func New(baseLogger *logger.Logger, configPath, version, commit string) *Service {
	statusService := status.NewStatusService(baseLogger, version, commit)
	return &Service{
		logger: logger.NewModuleLogger(baseLogger, "statuschan"),
		paths:  candidatePaths(configPath),
		build:  func() status.SystemStatus { return Snapshot(statusService) },
	}
}

func (s *Service) GetName() string { return "StatusChannel" }

// Start binds the channel. A failure to bind is logged and not fatal:
// the agent's job is monitoring, and `status` still has its HTTP and
// local fallbacks.
func (s *Service) Start(ctx context.Context) error {
	ln, bound, err := listen(s.paths)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Local status channel unavailable; `status` will fall back to the HTTP output")
		return nil
	}
	s.mu.Lock()
	s.listener, s.boundPath = ln, bound
	s.loopExited = make(chan struct{})
	exited := s.loopExited
	s.mu.Unlock()
	s.logger.Debug().Str("path", bound).Msg("Local status channel listening")
	go s.serve(ln, exited)
	return nil
}

// Shutdown closes the listener and removes the socket file.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	ln, bound, exited := s.listener, s.boundPath, s.loopExited
	s.listener, s.boundPath, s.loopExited = nil, "", nil
	s.mu.Unlock()
	if ln == nil {
		return nil
	}
	_ = ln.Close()
	select {
	case <-exited:
	case <-ctx.Done():
	}
	cleanup(bound)
	return nil
}

func (s *Service) serve(ln net.Listener, exited chan struct{}) {
	defer close(exited)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.answer(conn)
	}
}

// answer writes the status and closes. One connection at a time, each
// bounded by a deadline, so a client that never reads cannot pile up
// goroutines or hold the loop for longer than writeTimeout.
func (s *Service) answer(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := json.NewEncoder(conn).Encode(s.build()); err != nil {
		s.logger.Debug().Err(err).Msg("Local status client went away before reading the answer")
		return
	}
	waitUntilRead(conn, writeTimeout)
}

// waitUntilRead holds the connection open until the client has read the
// answer. Closing a Windows named pipe server end discards what the
// client has not read yet; FlushFileBuffers blocks until it has. The
// flush cannot be cancelled, so it runs aside and the loop moves on after
// the timeout; that goroutine ends when the client reads or disconnects.
func waitUntilRead(conn net.Conn, timeout time.Duration) {
	f, ok := conn.(interface{ Flush() error })
	if !ok {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = f.Flush()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Snapshot is the agent's status as the channel serves it: the process
// measurements of the status service, completed by what the agent keeps
// in memory (probe states, failed outputs, configuration watch), none of
// which needs an output to exist.
func Snapshot(svc *status.StatusService) status.SystemStatus {
	st := svc.GetSystemStatus()
	st.Agent.InstanceID = agentstate.GetAgentInstanceID()

	running := agentstate.RunningProbeStates()
	names := make([]string, 0, len(running))
	for name := range running {
		names = append(names, name)
	}
	sort.Strings(names)
	probes := make([]status.ProbeStatus, 0, len(names))
	failed := 0
	for _, name := range names {
		rs := running[name]
		p := status.ProbeStatus{Name: name, Status: "active", LastError: rs.LastError, MetricsCount: rs.LastPoints, LastUpdate: rs.LastCycle}
		switch {
		case rs.Health == "failed":
			p.Status = "error"
			failed++
		case rs.Health != "ok":
			p.Status = "inactive"
		}
		probes = append(probes, p)
	}
	st.Probes = probes
	st.Health = healthFor(st.Health, len(probes), failed)

	for name, f := range agentstate.GetStrategyFailures() {
		st.StrategyFailures = append(st.StrategyFailures, status.StrategyFailure{Strategy: name, Reason: f.Reason, Detail: f.Detail})
	}
	sort.Slice(st.StrategyFailures, func(i, j int) bool { return st.StrategyFailures[i].Strategy < st.StrategyFailures[j].Strategy })
	if w := agentstate.GetConfigWatchDisabled(); w != nil {
		st.ConfigWatch = &status.ConfigWatch{Reason: w.Reason, Detail: w.Detail}
	}
	return st
}

func healthFor(base status.HealthInfo, total, failed int) status.HealthInfo {
	switch {
	case failed == 0:
		return base
	case failed >= total/2:
		base.Status = "unhealthy"
		base.Message = fmt.Sprintf("%d of %d probes have errors", failed, total)
	default:
		base.Status = "degraded"
		base.Message = fmt.Sprintf("%d probe(s) have errors", failed)
	}
	return base
}

// Query asks the running agent for its status over the channel, trying
// the same candidate paths the agent binds from, in order.
func Query(configPath string) (*status.SystemStatus, error) {
	return queryPaths(candidatePaths(configPath))
}

func queryPaths(paths []string) (*status.SystemStatus, error) {
	var lastErr error
	for _, p := range paths {
		st, err := queryOne(p)
		if err == nil {
			return st, nil
		}
		lastErr = fmt.Errorf("%s: %w", p, err)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no local status channel path on this platform")
	}
	return nil, lastErr
}

func queryOne(path string) (*status.SystemStatus, error) {
	conn, err := dial(path, dialTimeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(writeTimeout + dialTimeout))
	var st status.SystemStatus
	if err := json.NewDecoder(io.LimitReader(conn, readLimit)).Decode(&st); err != nil {
		return nil, fmt.Errorf("reading the agent's answer: %w", err)
	}
	return &st, nil
}

func socketIn(dir string) string { return filepath.Join(dir, socketName) }

func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
