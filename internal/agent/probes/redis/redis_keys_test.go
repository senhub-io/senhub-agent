package redis

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

func readRESPCommand(r *bufio.Reader) ([]string, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading command array header: %w", err)
	}
	header = strings.TrimRight(header, "\r\n")
	if !strings.HasPrefix(header, "*") {
		return nil, fmt.Errorf("expected RESP array, got %q", header)
	}
	n, err := strconv.Atoi(header[1:])
	if err != nil || n < 0 {
		return nil, fmt.Errorf("invalid array length %q: %w", header, err)
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		bh, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading bulk header: %w", err)
		}
		bh = strings.TrimRight(bh, "\r\n")
		if !strings.HasPrefix(bh, "$") {
			return nil, fmt.Errorf("expected bulk string, got %q", bh)
		}
		l, err := strconv.Atoi(bh[1:])
		if err != nil || l < 0 {
			return nil, fmt.Errorf("invalid bulk length %q: %w", bh, err)
		}
		buf := make([]byte, l)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("reading bulk data: %w", err)
		}
		// trailing \r\n
		if _, err := r.ReadString('\n'); err != nil {
			return nil, fmt.Errorf("reading bulk trailing CRLF: %w", err)
		}
		parts[i] = string(buf)
	}
	return parts, nil
}

func writeBulkString(w io.Writer, body string) error {
	_, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(body), body)
	return err
}

func runFakeRedis(infoBody, cmdStatsBody string) (net.Conn, <-chan error) {
	client, server := net.Pipe()
	errCh := make(chan error, 1)

	go func() {
		defer server.Close()
		br := bufio.NewReader(server)

		parts, err := readRESPCommand(br)
		if err != nil {
			errCh <- err
			return
		}
		if len(parts) < 2 || parts[0] != "INFO" || parts[1] != "all" {
			errCh <- fmt.Errorf("first command = %v, want [INFO all]", parts)
			return
		}
		if err := writeBulkString(server, infoBody); err != nil {
			errCh <- err
			return
		}

		parts, err = readRESPCommand(br)
		if err != nil {
			errCh <- err
			return
		}
		if len(parts) < 2 || parts[0] != "INFO" || parts[1] != "commandstats" {
			errCh <- fmt.Errorf("second command = %v, want [INFO commandstats]", parts)
			return
		}
		if err := writeBulkString(server, cmdStatsBody); err != nil {
			errCh <- err
			return
		}

		errCh <- nil
	}()

	return client, errCh
}

func assertAllPointsHaveDBInstanceID(t *testing.T, points []data_store.DataPoint, wantID string) {
	t.Helper()
	for _, pt := range points {
		found := false
		for _, tg := range pt.Tags {
			if tg.Key == "db.instance.id" {
				if tg.Value != wantID {
					t.Errorf("datapoint %s has db.instance.id %q, want %q", pt.Name, tg.Value, wantID)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("datapoint %s is missing db.instance.id", pt.Name)
		}
	}
}

func assertMetricTypeStatus(t *testing.T, pt data_store.DataPoint) {
	t.Helper()
	for _, tg := range pt.Tags {
		if tg.Key == "metric_type" && tg.Value == "status" {
			return
		}
	}
	t.Errorf("datapoint %s missing metric_type=status", pt.Name)
}

func assertCheckResult(t *testing.T, points []data_store.DataPoint, want float64) {
	t.Helper()
	for _, pt := range points {
		if pt.Name == dbcommon.CheckResultMetric {
			if pt.Value != want {
				t.Errorf("check result value = %v, want %v", pt.Value, want)
			}
			assertMetricTypeStatus(t, pt)
			return
		}
	}
	t.Fatal("check result datapoint not found")
}

func TestCollectKeysOnSuccess(t *testing.T) {
	infoBody := "redis_version:7.2.0\r\nuptime_in_seconds:100\r\nrole:master\r\nconnected_clients:1\r\n"
	cmdStatsBody := "cmdstat_get:calls=10,usec=100,usec_per_call=10.00,rejected_calls=0,failed_calls=0\r\n"

	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	probeIntf, err := NewRedisProbe(map[string]interface{}{
		"host":    "127.0.0.1",
		"port":    6379,
		"timeout": 30,
	}, baseLogger)
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	p := probeIntf.(*redisProbe)

	client, serverErrCh := runFakeRedis(infoBody, cmdStatsBody)
	p.dialFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return client, nil
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect unexpected error: %v", err)
	}

	select {
	case err := <-serverErrCh:
		if err != nil {
			t.Fatalf("fake redis server: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake redis server did not finish")
	}

	expectedID := dbcommon.FallbackInstanceID("redis", "127.0.0.1", 6379, dbcommon.HostID())
	assertAllPointsHaveDBInstanceID(t, points, expectedID)
	assertCheckResult(t, points, float64(dbcommon.CheckOK))
}

func TestCollectKeysOnConnectionFailure(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	probeIntf, err := NewRedisProbe(map[string]interface{}{
		"host": "127.0.0.1",
		"port": 6379,
	}, baseLogger)
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	p := probeIntf.(*redisProbe)

	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	p.dialFn = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, dialErr
	}

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect should return nil on dial failure, got %v", err)
	}

	expectedID := dbcommon.FallbackInstanceID("redis", "127.0.0.1", 6379, dbcommon.HostID())
	assertAllPointsHaveDBInstanceID(t, points, expectedID)

	var upPoint, checkPoint *data_store.DataPoint
	for i := range points {
		switch points[i].Name {
		case "senhub.db.up":
			upPoint = &points[i]
		case dbcommon.CheckResultMetric:
			checkPoint = &points[i]
		}
	}

	if upPoint == nil {
		t.Fatal("missing senhub.db.up")
	}
	if upPoint.Value != 0 {
		t.Errorf("up value = %v, want 0", upPoint.Value)
	}

	if checkPoint == nil {
		t.Fatal("missing check result")
	}
	if checkPoint.Value != float64(dbcommon.CheckTargetDown) {
		t.Errorf("check result = %v, want %v", checkPoint.Value, float64(dbcommon.CheckTargetDown))
	}
	assertMetricTypeStatus(t, *checkPoint)
}

func TestDownServerWarnsOncePerOutage(t *testing.T) {
	probeIntf, err := NewRedisProbe(map[string]interface{}{
		"host":    "127.0.0.1",
		"port":    6379,
		"timeout": 30,
	}, logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewRedisProbe: %v", err)
	}
	p := probeIntf.(*redisProbe)
	var buf bytes.Buffer
	base := zerolog.New(&buf).Level(zerolog.DebugLevel)
	p.moduleLogger = logger.NewModuleLogger(&base, "probe.test")

	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	p.dialFn = func(string, string, time.Duration) (net.Conn, error) { return nil, dialErr }
	for i := 0; i < 10; i++ {
		points, err := p.Collect()
		if err != nil {
			t.Fatalf("Collect should not return an error for a down server, got %v", err)
		}
		assertCheckResult(t, points, float64(dbcommon.ClassifyError(dialErr)))
	}

	client, serverErrCh := runFakeRedis("redis_version:7.2.0\r\nrole:master\r\n", "")
	p.dialFn = func(string, string, time.Duration) (net.Conn, error) { return client, nil }
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	<-serverErrCh

	if got := strings.Count(buf.String(), `"level":"warn"`); got != 1 {
		t.Errorf("%d warnings over 10 failed cycles, want 1\n%s", got, buf.String())
	}
	if got := strings.Count(buf.String(), `"level":"info"`); got != 1 {
		t.Errorf("%d recovery lines, want 1\n%s", got, buf.String())
	}
}
