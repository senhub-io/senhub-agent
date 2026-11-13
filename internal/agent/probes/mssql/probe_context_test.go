package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

// hangingConn is a server that accepted the connection and never
// answers: Ping blocks until its context ends.
type hangingConn struct{}

func (hangingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (hangingConn) Close() error                        { return nil }
func (hangingConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }
func (hangingConn) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

type hangingConnector struct{}

func (hangingConnector) Connect(context.Context) (driver.Conn, error) { return hangingConn{}, nil }
func (hangingConnector) Driver() driver.Driver                        { return nil }

// A server that stops answering must not hold a cancelled collection for
// the query timeout, and the interruption is not reported as the server
// being down.
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	probe, err := NewMSSQLProbe(map[string]interface{}{"host": "testhost", "port": 1433}, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("NewMSSQLProbe: %v", err)
	}
	p := probe.(*MSSQLProbe)
	p.db = sql.OpenDB(hangingConnector{})
	t.Cleanup(func() { _ = p.db.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	started := time.Now()
	points, err := p.CollectContext(ctx)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("CollectContext took %v after its context was cancelled", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
	if len(points) != 0 {
		t.Fatalf("an interrupted cycle emitted %d datapoints, want none (no false up=0)", len(points))
	}
}

// The poller detects cancellation support by type assertion; a lost
// method would silently fall back to the uncancellable Collect.
var _ types.ContextCollector = (*MSSQLProbe)(nil)
