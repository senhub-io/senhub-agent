// Package bridge wraps a long-lived JT400 subprocess so Go code can issue
// JDBC queries against IBM i without a cgo driver.
//
// The Go side spawns a Java process (Jt400Runner) once, holds a single JDBC
// connection through it, and exchanges one-line messages over stdin/stdout:
//
//	→ SQL text, terminated by '\n'     (no embedded newlines)
//	← one JSON line terminated by '\n':
//	     {"ok":true,"columns":[...],"rows":[[...], ...]}
//	     {"ok":false,"error":"..."}
//
// Startup handshake: the runner emits {"ok":true,"event":"ready"} once the
// JDBC connection is established.
//
// Credentials are passed only through the child process environment —
// never on the command line, never written to disk by this package.
//
// The whole package is designed to be copied into
// senhub-agent/internal/agent/probes/ibmi/bridge/ at integration time. It
// takes its logger as a *zerolog.Logger so the swap from
// senhub4i.go/pkg/probe.ModuleLogger to senhub-agent's ModuleLogger is
// zero-cost.
package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Config controls how the bridge subprocess is launched.
type Config struct {
	// Host is the IBM i hostname or IP. Required.
	Host string
	// User is the IBM i user profile. Required.
	User string
	// Password is the user's password. Required.
	Password string

	// JavaHome is the path to a JRE/JDK installation (bin/java must exist).
	// If empty, defaults to the JAVA_HOME environment variable, then to a
	// hard-coded Temurin 17 fallback suitable for local development.
	JavaHome string

	// RunnerDir is the directory containing Jt400Runner.class and jt400.jar.
	// Required — there is deliberately no automatic discovery. In the POC
	// this points at internal/probes/ibmi/bridge/ during development and at
	// /opt/senhub4i/bridge/ inside the Docker image.
	RunnerDir string

	// StartupTimeout bounds how long New waits for the "ready" handshake.
	// Zero means 15 seconds.
	StartupTimeout time.Duration

	// QueryTimeout bounds how long Query will wait for a single response
	// line before considering the bridge stuck and returning an error.
	// Zero means 10 seconds.
	QueryTimeout time.Duration
}

// Result is the decoded shape of a successful query response.
type Result struct {
	Columns []string
	Rows    [][]*string
}

// Bridge owns the subprocess and serialises query access to it.
//
// A Bridge is safe for concurrent Query callers: queries are serialised
// internally behind a mutex. That is deliberate — the stdin/stdout protocol
// is strictly request/response and cannot be multiplexed without framing
// changes on the Java side.
type Bridge struct {
	cfg    Config
	logger *zerolog.Logger

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu     sync.Mutex
	closed bool
}

// wireResponse is the on-wire shape of a single JSON line emitted by
// Jt400Runner. Unexported because callers get a decoded Result instead.
type wireResponse struct {
	OK      bool        `json:"ok"`
	Event   string      `json:"event,omitempty"`
	Error   string      `json:"error,omitempty"`
	Columns []string    `json:"columns,omitempty"`
	Rows    [][]*string `json:"rows,omitempty"`
}

// New spawns the runner subprocess and waits for its startup handshake.
//
// On success the caller owns the returned *Bridge and must eventually call
// Close to release OS resources. On failure, New leaves no child process
// behind.
func New(ctx context.Context, cfg Config, logger *zerolog.Logger) (*Bridge, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	javaBin := filepath.Join(cfg.JavaHome, "bin", "java")
	if _, err := os.Stat(javaBin); err != nil {
		return nil, fmt.Errorf("java binary not found at %s: %w", javaBin, err)
	}

	cmd := exec.CommandContext(ctx, javaBin, "-cp", "jt400.jar:.", "Jt400Runner")
	cmd.Dir = cfg.RunnerDir
	cmd.Env = append(os.Environ(),
		"PUB400_HOST="+cfg.Host,
		"PUB400_USER="+cfg.User,
		"PUB400_PASSWORD="+cfg.Password,
	)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("allocating stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("allocating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting runner: %w", err)
	}

	b := &Bridge{
		cfg:    cfg,
		logger: logger,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
	}

	// Read the handshake under the configured startup timeout. If the
	// handshake never arrives we tear the subprocess down before returning.
	handshakeCtx, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()

	line, err := readLineCtx(handshakeCtx, b.stdout)
	if err != nil {
		b.forceKill()
		return nil, fmt.Errorf("reading handshake: %w", err)
	}
	var resp wireResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		b.forceKill()
		return nil, fmt.Errorf("parsing handshake %q: %w", line, err)
	}
	if !resp.OK {
		b.forceKill()
		return nil, fmt.Errorf("runner failed to start: %s", resp.Error)
	}
	if resp.Event != "ready" {
		b.forceKill()
		return nil, fmt.Errorf("unexpected handshake event %q", resp.Event)
	}

	if logger != nil {
		logger.Info().
			Str("host", cfg.Host).
			Str("user", cfg.User).
			Msg("jt400 bridge ready")
	}
	return b, nil
}

// Query sends one SQL statement to the runner and returns its decoded
// response. Queries are serialised: concurrent callers wait their turn.
//
// The provided context bounds the wait time for the response line. On
// context cancellation the Bridge becomes permanently unusable — we have
// no safe way to abort a query in flight on the Java side without killing
// the subprocess, so we do exactly that, and subsequent Query calls fail.
func (b *Bridge) Query(ctx context.Context, sql string) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, b.cfg.QueryTimeout)
	defer cancel()

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, errors.New("bridge is closed")
	}
	if containsNewline(sql) {
		return nil, errors.New("sql statement must be single-line (no embedded newlines)")
	}

	if _, err := io.WriteString(b.stdin, sql+"\n"); err != nil {
		return nil, fmt.Errorf("writing query: %w", err)
	}

	line, err := readLineCtx(queryCtx, b.stdout)
	if err != nil {
		b.forceKill()
		b.closed = true
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var resp wireResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("parsing response %q: %w", line, err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("runner error: %s", resp.Error)
	}
	return &Result{Columns: resp.Columns, Rows: resp.Rows}, nil
}

// Close shuts the subprocess down cleanly: it sends an empty line, waits
// up to 5 seconds for exit, then force-kills. Close is idempotent.
func (b *Bridge) Close(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	b.closed = true

	_, _ = io.WriteString(b.stdin, "\n")
	_ = b.stdin.Close()

	done := make(chan error, 1)
	go func() { done <- b.cmd.Wait() }()

	timeout := 5 * time.Second
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = b.cmd.Process.Kill()
		<-done
		return errors.New("runner did not exit cleanly; killed")
	case <-ctx.Done():
		_ = b.cmd.Process.Kill()
		<-done
		return ctx.Err()
	}
}

// forceKill terminates the child process without serialisation. Only call
// from code paths that own the construction or error path and know no
// other goroutine is racing for mu.
func (b *Bridge) forceKill() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
	}
}

// validate enforces the minimum set of fields required by New.
func (c *Config) validate() error {
	if c.Host == "" {
		return errors.New("bridge config: Host is required")
	}
	if c.User == "" {
		return errors.New("bridge config: User is required")
	}
	if c.Password == "" {
		return errors.New("bridge config: Password is required")
	}
	if c.RunnerDir == "" {
		return errors.New("bridge config: RunnerDir is required")
	}
	return nil
}

// applyDefaults fills in optional fields with sensible defaults. Mutates
// the receiver — call after validate.
func (c *Config) applyDefaults() {
	if c.JavaHome == "" {
		if env := os.Getenv("JAVA_HOME"); env != "" {
			c.JavaHome = env
		} else {
			c.JavaHome = "/Library/Java/JavaVirtualMachines/temurin-17.jdk/Contents/Home"
		}
	}
	if c.StartupTimeout == 0 {
		c.StartupTimeout = 15 * time.Second
	}
	if c.QueryTimeout == 0 {
		c.QueryTimeout = 10 * time.Second
	}
}

// readLineCtx reads one '\n'-terminated line, honouring ctx cancellation.
// It uses a goroutine + select because bufio.Reader.ReadString is blocking
// and has no native cancellation support.
func readLineCtx(ctx context.Context, r *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadString('\n')
		ch <- result{line: line, err: err}
	}()
	select {
	case res := <-ch:
		return res.line, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func containsNewline(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return true
		}
	}
	return false
}
