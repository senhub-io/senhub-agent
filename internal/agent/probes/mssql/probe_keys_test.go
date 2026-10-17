package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"senhub-agent.go/internal/agent/cliArgs"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type fakeConnector struct {
	pingErr error
}

func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeConn{pingErr: c.pingErr}, nil
}

func (c *fakeConnector) Driver() driver.Driver {
	return fakeDriver{}
}

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return &fakeConn{}, nil
}

type fakeConn struct {
	pingErr error
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not implemented")
}

func (c *fakeConn) Close() error {
	return nil
}

func (c *fakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("Begin not implemented")
}

func (c *fakeConn) Ping(context.Context) error {
	return c.pingErr
}

func (c *fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{}, nil
}

type fakeRows struct{}

func (fakeRows) Columns() []string {
	return nil
}

func (fakeRows) Close() error {
	return nil
}

func (fakeRows) Next([]driver.Value) error {
	return io.EOF
}

func hasTag(tags []tags.Tag, key, want string) bool {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value == want
		}
	}
	return false
}

func TestCollect_Success_CarriesIdentityAndCheckResultZero(t *testing.T) {
	probe, err := NewMSSQLProbe(map[string]interface{}{"host": "testhost", "port": 1433}, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("NewMSSQLProbe: %v", err)
	}
	p, ok := probe.(*MSSQLProbe)
	if !ok {
		t.Fatalf("probe is %T, want *MSSQLProbe", probe)
	}
	p.db = sql.OpenDB(&fakeConnector{pingErr: nil})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(points) < 2 {
		t.Fatalf("expected at least up and check result, got %d: %+v", len(points), points)
	}

	wantInstanceID := p.entitySource.instanceID
	for _, dp := range points {
		if !hasTag(dp.Tags, idKeyDBInstance, wantInstanceID) {
			t.Errorf("datapoint %q missing/incorrect %s: got %v", dp.Name, idKeyDBInstance, dp.Tags)
		}
		if !hasTag(dp.Tags, idKeyDBSystem, dbSystemMSSQL) {
			t.Errorf("datapoint %q missing %s: got %v", dp.Name, idKeyDBSystem, dp.Tags)
		}
		if dp.Name == dbcommon.CheckResultMetric {
			if !hasTag(dp.Tags, "metric_type", metricTypeStatus) {
				t.Errorf("check result metric_type = %v, want status", dp.Tags)
			}
		}
	}

	var checkVal float64
	found := false
	for _, dp := range points {
		if dp.Name == dbcommon.CheckResultMetric {
			checkVal = dp.Value
			found = true
			break
		}
	}
	if !found {
		t.Fatal("check result metric not found")
	}
	if checkVal != 0 {
		t.Errorf("check result = %v, want 0", checkVal)
	}
}

func TestCollect_PingFailure_CheckResultClassifiedAndKeysPresent(t *testing.T) {
	probe, err := NewMSSQLProbe(map[string]interface{}{"host": "testhost", "port": 1433}, logger.NewLogger(&cliArgs.ParsedArgs{}))
	if err != nil {
		t.Fatalf("NewMSSQLProbe: %v", err)
	}
	p, ok := probe.(*MSSQLProbe)
	if !ok {
		t.Fatalf("probe is %T, want *MSSQLProbe", probe)
	}
	p.db = sql.OpenDB(&fakeConnector{
		pingErr: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
	})

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantInstanceID := p.entitySource.instanceID
	var upVal, checkVal float64
	foundUp := false
	foundCheck := false

	for _, dp := range points {
		switch dp.Name {
		case "senhub.db.up":
			foundUp = true
			upVal = dp.Value
			if !hasTag(dp.Tags, idKeyDBInstance, wantInstanceID) {
				t.Errorf("up missing/incorrect %s: tags=%v", idKeyDBInstance, dp.Tags)
			}
			if !hasTag(dp.Tags, idKeyDBSystem, dbSystemMSSQL) {
				t.Errorf("up missing %s", idKeyDBSystem)
			}
		case dbcommon.CheckResultMetric:
			foundCheck = true
			checkVal = dp.Value
			if !hasTag(dp.Tags, idKeyDBInstance, wantInstanceID) {
				t.Errorf("check result missing/incorrect %s", idKeyDBInstance)
			}
			if !hasTag(dp.Tags, idKeyDBSystem, dbSystemMSSQL) {
				t.Errorf("check result missing %s", idKeyDBSystem)
			}
			if !hasTag(dp.Tags, "metric_type", metricTypeStatus) {
				t.Errorf("check result metric_type = %v, want status", dp.Tags)
			}
		}
	}

	if !foundUp {
		t.Fatal("up metric not found")
	}
	if !foundCheck {
		t.Fatal("check result metric not found")
	}
	if upVal != 0 {
		t.Errorf("senhub.db.up = %v, want 0 unchanged", upVal)
	}
	wantClass := float64(dbcommon.CheckTargetDown)
	if checkVal != wantClass {
		t.Errorf("check result = %v, want %v", checkVal, wantClass)
	}
}
