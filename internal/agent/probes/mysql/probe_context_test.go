package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"
)

// stallingConn answers the ping, then never answers a query: the shape
// of a server that accepted the session and stalled.
type stallingConn struct{}

func (stallingConn) Ping(context.Context) error          { return nil }
func (stallingConn) Close() error                        { return nil }
func (stallingConn) Begin() (driver.Tx, error)           { return nil, errors.New("not supported") }
func (stallingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (stallingConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type stallingConnector struct{}

func (stallingConnector) Connect(context.Context) (driver.Conn, error) { return stallingConn{}, nil }
func (stallingConnector) Driver() driver.Driver                        { return nil }

// A cancelled cycle returns promptly with the cancellation, publishes
// nothing, and does not pin the interrupted server-UUID query as the
// answer (which would leave the entity on the host:port fallback for
// the life of the probe).
func TestCollectContext_CancelledReturnsPromptly(t *testing.T) {
	p := newProbeForTest(t, map[string]interface{}{
		"host":     "127.0.0.1",
		"port":     3306,
		"username": "u",
		"password": "p",
		"timeout":  "30s",
	})
	p.db = sql.OpenDB(stallingConnector{})
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
		t.Fatalf("an interrupted cycle emitted %d datapoints, want none", len(points))
	}
	if p.uuidFetched {
		t.Fatal("an interrupted server-UUID query was recorded as fetched")
	}
}
