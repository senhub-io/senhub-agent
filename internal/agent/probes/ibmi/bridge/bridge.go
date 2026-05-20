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
// The bridge supervises the subprocess: if it exits (TCP reset by the
// LPAR, OOM, hardware reboot, …) the next Query triggers an automatic
// respawn + handshake before retrying. Callers see the failure only when
// respawn itself fails.
//
// NativeRunner vs JVM: when Config.NativeRunner is set, the bridge spawns
// a GraalVM native-image compiled Jt400Runner binary directly — no JRE
// required at runtime. Otherwise it falls back to `java -cp jt400.jar:.
// Jt400Runner`.
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
	"runtime"
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
	// Ignored when NativeRunner is set.
	JavaHome string

	// NativeRunner is an optional path to a GraalVM native-image compiled
	// jt400runner binary. When set, the bridge spawns this binary directly
	// instead of `java -cp jt400.jar:. Jt400Runner` — no JRE required
	// at runtime. The native binary still speaks the same line-oriented
	// stdin/stdout protocol and reads credentials from PUB400_* env vars.
	NativeRunner string

	// RunnerDir is the directory containing Jt400Runner.class and jt400.jar.
	// Required on the JVM path; optional when NativeRunner is set (used as
	// working directory for the native binary).
	RunnerDir string

	// StartupTimeout bounds how long spawn waits for the "ready" handshake.
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
// changes on the runner side.
//
// The bridge has three lifecycle states:
//   - live: cmd/stdin/stdout are wired and the subprocess is responsive
//   - dead: subprocess has exited for an external reason (TCP reset,
//     OOM, etc.); next Query will respawn before retrying
//   - closed: the user called Close — permanent, no further Queries allowed
type Bridge struct {
	cfg    Config
	logger *zerolog.Logger

	mu           sync.Mutex
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdout       *bufio.Reader
	dead         bool // subprocess has exited; needs respawn before next query
	closedByUser bool // Close() was called; no more queries ever

	respawnCount int // bookkeeping, exposed through Stats for tests/metrics
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
// behind. The provided ctx bounds the initial spawn+handshake; the
// subprocess itself is not tied to ctx (it outlives the call).
func New(ctx context.Context, cfg Config, logger *zerolog.Logger) (*Bridge, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()

	b := &Bridge{cfg: cfg, logger: logger}
	if err := b.spawn(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

// spawn launches (or re-launches) the subprocess, performs the handshake
// and wires b.cmd/stdin/stdout. Caller must hold b.mu, except during New
// where no concurrent access is possible yet.
//
// The subprocess is NOT tied to ctx: we want it to outlive a potentially
// short caller context. ctx is used only for the handshake timeout.
func (b *Bridge) spawn(ctx context.Context) error {
	res := ResolveRuntime(b.cfg)
	if res.Err != nil {
		return res.Err
	}

	var cmd *exec.Cmd
	switch res.Mode {
	case RuntimeModeNative:
		cmd = exec.Command(res.NativeRunner)
	case RuntimeModeJava:
		// Use the platform path-list separator so the classpath is valid
		// on Windows (;) as well as Unix (:).
		classpath := "jt400.jar" + string(os.PathListSeparator) + "."
		cmd = exec.Command(res.JavaBin, "-cp", classpath, "Jt400Runner")
	default:
		return fmt.Errorf("internal: resolver returned no Mode despite no Err — this is a bug")
	}
	cmd.Dir = res.RunnerDir
	cmd.Env = append(os.Environ(),
		"PUB400_HOST="+b.cfg.Host,
		"PUB400_USER="+b.cfg.User,
		"PUB400_PASSWORD="+b.cfg.Password,
	)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("allocating stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("allocating stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting runner: %w", err)
	}

	reader := bufio.NewReader(stdoutPipe)

	handshakeCtx, cancel := context.WithTimeout(ctx, b.cfg.StartupTimeout)
	defer cancel()

	line, err := readLineCtx(handshakeCtx, reader)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("reading handshake: %w", err)
	}
	var resp wireResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("parsing handshake %q: %w", line, err)
	}
	if !resp.OK {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("runner failed to start: %s", resp.Error)
	}
	if resp.Event != "ready" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("unexpected handshake event %q", resp.Event)
	}

	b.cmd = cmd
	b.stdin = stdin
	b.stdout = reader
	b.dead = false

	if b.logger != nil {
		event := b.logger.Info().
			Str("host", b.cfg.Host).
			Str("user", b.cfg.User)
		if b.respawnCount > 0 {
			event = event.Int("respawn_count", b.respawnCount)
		}
		event.Msg("jt400 bridge ready")
	}
	return nil
}

// Query sends one SQL statement to the runner and returns its decoded
// response. Queries are serialised: concurrent callers wait their turn.
//
// If the subprocess has died since the last query (EOF on stdout,
// kill signal, SIGPIPE, …), Query triggers a single automatic respawn
// and retries. Respawn itself uses a background context so a short
// ctx from the caller does not cut off the startup handshake.
//
// Semantic SQL errors from the runner ({"ok":false,"error":"…"}) are
// returned as errors but do NOT mark the bridge dead — the subprocess
// is still healthy and ready for the next query.
func (b *Bridge) Query(ctx context.Context, sql string) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closedByUser {
		return nil, errors.New("bridge is closed")
	}
	if containsNewline(sql) {
		return nil, errors.New("sql statement must be single-line (no embedded newlines)")
	}

	// If the subprocess died previously, try to respawn before we write.
	// We use a background context with a generous deadline so a 1s
	// per-collector query timeout does not cut off the 15s handshake.
	if b.dead {
		respawnCtx, cancel := context.WithTimeout(context.Background(), b.cfg.StartupTimeout+5*time.Second)
		err := b.respawnLocked(respawnCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("bridge dead and respawn failed: %w", err)
		}
	}

	queryCtx, cancel := context.WithTimeout(ctx, b.cfg.QueryTimeout)
	defer cancel()

	if _, err := io.WriteString(b.stdin, sql+"\n"); err != nil {
		b.markDeadLocked("write failed: " + err.Error())
		return nil, fmt.Errorf("writing query: %w", err)
	}

	line, err := readLineCtx(queryCtx, b.stdout)
	if err != nil {
		b.markDeadLocked("read response failed: " + err.Error())
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var resp wireResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		// Malformed wire data — we cannot trust the stream anymore.
		// Treat as dead so the next call respawns.
		b.markDeadLocked("malformed response")
		return nil, fmt.Errorf("parsing response %q: %w", line, err)
	}
	if !resp.OK {
		// Semantic SQL error — subprocess still healthy.
		return nil, fmt.Errorf("runner error: %s", resp.Error)
	}
	return &Result{Columns: resp.Columns, Rows: resp.Rows}, nil
}

// markDeadLocked transitions the bridge to the "dead" state and ensures
// the subprocess is reaped. Idempotent. Must be called with b.mu held.
func (b *Bridge) markDeadLocked(reason string) {
	if b.dead {
		return
	}
	b.dead = true
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_ = b.cmd.Wait()
	}
	if b.logger != nil {
		b.logger.Warn().
			Str("reason", reason).
			Msg("jt400 bridge subprocess died; respawn on next query")
	}
}

// respawnLocked re-launches the subprocess and performs the handshake.
// Increments the respawn counter on success. Must be called with b.mu
// held and b.dead == true.
func (b *Bridge) respawnLocked(ctx context.Context) error {
	b.respawnCount++
	return b.spawn(ctx)
}

// RespawnCount returns the number of times the subprocess has been
// automatically restarted since New. Exposed for probe health metrics.
func (b *Bridge) RespawnCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.respawnCount
}

// Close shuts the subprocess down cleanly: it sends an empty line, waits
// up to 5 seconds for exit, then force-kills. Close is idempotent.
func (b *Bridge) Close(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closedByUser {
		return nil
	}
	b.closedByUser = true

	// If the subprocess already died and we haven't respawned yet,
	// there's nothing to shut down.
	if b.dead || b.cmd == nil {
		return nil
	}

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
	if c.NativeRunner == "" && c.RunnerDir == "" {
		return errors.New("bridge config: either NativeRunner or RunnerDir is required")
	}
	return nil
}

// applyDefaults fills in optional fields with sensible defaults. Mutates
// the receiver — call after validate.
func (c *Config) applyDefaults() {
	if c.NativeRunner == "" && c.JavaHome == "" {
		if env := os.Getenv("JAVA_HOME"); env != "" {
			c.JavaHome = env
		} else if runtime.GOOS == "darwin" {
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
