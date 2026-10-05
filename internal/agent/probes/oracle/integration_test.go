//go:build integration

package oracle

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	go_ora "github.com/sijms/go-ora/v2"

	"senhub-agent.go/internal/agent/services/data_store"
)

// The suite runs against any Oracle the environment points at, and skips
// when none is set. Either one DSN:
//
//	ORACLE_TEST_DSN=oracle://user:password@host:1521/SERVICE
//
// or the parts: ORACLE_TEST_HOST, ORACLE_TEST_PORT (default 1521),
// ORACLE_TEST_SERVICE, ORACLE_TEST_USER, ORACLE_TEST_PASSWORD. The user
// holds the grants listed on the probe page. testdata/run-integration.sh
// starts a container and sets all of this.
type target struct {
	host, service, user, password string
	port                          int
}

func targetFromEnv(t *testing.T) target {
	t.Helper()

	if dsn := os.Getenv("ORACLE_TEST_DSN"); dsn != "" {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("ORACLE_TEST_DSN is not a URL: %v", err)
		}
		host, portStr, err := net.SplitHostPort(u.Host)
		if err != nil {
			host, portStr = u.Hostname(), strconv.Itoa(defaultPort)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			t.Fatalf("ORACLE_TEST_DSN has a bad port %q", portStr)
		}
		pass, _ := u.User.Password()
		return target{
			host:     host,
			port:     port,
			service:  strings.Trim(u.Path, "/"),
			user:     u.User.Username(),
			password: pass,
		}
	}

	host := os.Getenv("ORACLE_TEST_HOST")
	service := os.Getenv("ORACLE_TEST_SERVICE")
	user := os.Getenv("ORACLE_TEST_USER")
	if host == "" || service == "" || user == "" {
		t.Skip("no Oracle to test against: set ORACLE_TEST_DSN, or ORACLE_TEST_HOST/SERVICE/USER/PASSWORD")
	}
	port := defaultPort
	if v := os.Getenv("ORACLE_TEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("ORACLE_TEST_PORT %q is not a number", v)
		}
		port = p
	}
	return target{host: host, port: port, service: service, user: user, password: os.Getenv("ORACLE_TEST_PASSWORD")}
}

func (tg target) params() map[string]interface{} {
	return map[string]interface{}{
		"host":         tg.host,
		"port":         tg.port,
		"service_name": tg.service,
		"username":     tg.user,
		"password":     tg.password,
	}
}

func startProbe(t *testing.T, tg target) *oracleProbe {
	t.Helper()
	p, err := NewOracleProbe(tg.params(), testLogger())
	if err != nil {
		t.Fatalf("NewOracleProbe: %v", err)
	}
	op := p.(*oracleProbe)
	op.SetName("integration-oracle")
	if err := op.OnStart(nil); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	t.Cleanup(func() { _ = op.OnShutdown(nil) })
	return op
}

func collect(t *testing.T, op *oracleProbe) []data_store.DataPoint {
	t.Helper()
	points, err := op.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return points
}

func named(points []data_store.DataPoint, name string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, dp := range points {
		if dp.Name == name {
			out = append(out, dp)
		}
	}
	return out
}

func tagValue(dp data_store.DataPoint, key string) string {
	for _, tg := range dp.Tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

func one(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	got := named(points, name)
	if len(got) != 1 {
		t.Fatalf("%s: %d datapoints, want exactly 1", name, len(got))
	}
	return got[0]
}

func TestIntegration_Collect(t *testing.T) {
	tg := targetFromEnv(t)
	op := startProbe(t, tg)

	// An idle second session, so v$session holds an INACTIVE row besides
	// the probe's own ACTIVE one.
	idle, err := openDB(go_ora.BuildUrl(tg.host, tg.port, tg.service, tg.user, tg.password, nil))
	if err != nil {
		t.Fatalf("opening the idle session: %v", err)
	}
	defer idle.Close()
	conn, err := idle.Conn(context.Background())
	if err != nil {
		t.Fatalf("idle session: %v", err)
	}
	defer conn.Close()
	var one1 int
	if err := conn.QueryRowContext(context.Background(), "SELECT 1 FROM dual").Scan(&one1); err != nil {
		t.Fatalf("idle session query: %v", err)
	}

	points := collect(t, op)

	if up := one(t, points, "senhub.db.up"); up.Value != 1 {
		t.Fatalf("senhub.db.up = %v, want 1: the probe could not log in as %s to %s:%d/%s", up.Value, tg.user, tg.host, tg.port, tg.service)
	}

	t.Run("every documented metric is emitted", func(t *testing.T) {
		for _, name := range []string{
			"senhub.db.up",
			"oracle.sessions.count",
			"oracle.sessions.limit",
			"oracle.physical.reads",
			"oracle.physical.writes",
			"oracle.buffer.cache.hit_ratio",
			"oracle.sga.total",
			"oracle.pga.total",
			"oracle.tablespace.used",
			"oracle.tablespace.total",
			"oracle.wait_class.total",
			"oracle.enqueue_deadlocks",
		} {
			if len(named(points, name)) == 0 {
				t.Errorf("%s not emitted", name)
			}
		}
	})

	t.Run("values are sane", func(t *testing.T) {
		if v := one(t, points, "oracle.sessions.limit").Value; v < 1 {
			t.Errorf("oracle.sessions.limit = %v, want >= 1", v)
		}
		for _, name := range []string{"oracle.physical.reads", "oracle.physical.writes", "oracle.enqueue_deadlocks"} {
			if v := one(t, points, name).Value; v < 0 {
				t.Errorf("%s = %v, want >= 0", name, v)
			}
		}
		if v := one(t, points, "oracle.buffer.cache.hit_ratio").Value; v < 0 || v > 100 {
			t.Errorf("oracle.buffer.cache.hit_ratio = %v, want within [0,100]", v)
		}
		for _, name := range []string{"oracle.sga.total", "oracle.pga.total"} {
			if v := one(t, points, name).Value; v <= 0 {
				t.Errorf("%s = %v, want > 0", name, v)
			}
		}
		for _, dp := range named(points, "oracle.wait_class.total") {
			if dp.Value < 0 || tagValue(dp, "wait_class") == "" {
				t.Errorf("oracle.wait_class.total = %v with wait_class %q", dp.Value, tagValue(dp, "wait_class"))
			}
		}
	})

	t.Run("sessions carry active and inactive", func(t *testing.T) {
		seen := map[string]float64{}
		for _, dp := range named(points, "oracle.sessions.count") {
			seen[tagValue(dp, "status")] = dp.Value
		}
		for _, status := range []string{"active", "inactive"} {
			if v, ok := seen[status]; !ok || v < 1 {
				t.Errorf("oracle.sessions.count{status=%s} = %v (present %v), want >= 1; got %v", status, v, ok, seen)
			}
		}
	})

	t.Run("tablespaces report used and total", func(t *testing.T) {
		used := map[string]float64{}
		total := map[string]float64{}
		for _, dp := range named(points, "oracle.tablespace.used") {
			used[tagValue(dp, "tablespace")] = dp.Value
		}
		for _, dp := range named(points, "oracle.tablespace.total") {
			total[tagValue(dp, "tablespace")] = dp.Value
		}
		if len(total) == 0 {
			t.Fatal("no tablespace emitted")
		}
		for name, tot := range total {
			u, ok := used[name]
			if !ok {
				t.Errorf("tablespace %s has a total but no used", name)
				continue
			}
			if tot <= 0 || u < 0 || u > tot {
				t.Errorf("tablespace %s: used %v, total %v", name, u, tot)
			}
		}
	})

	t.Run("points carry the standard tags", func(t *testing.T) {
		for _, dp := range points {
			if !hasTag(dp.Tags, "db.system.name", "oracle") ||
				!hasTag(dp.Tags, "server.address", tg.host) ||
				!hasTag(dp.Tags, "server.port", fmt.Sprint(tg.port)) ||
				!hasTag(dp.Tags, "probe_type", ProbeType) {
				t.Errorf("%s: missing a standard tag in %v", dp.Name, dp.Tags)
			}
		}
	})
}

// A password longer than 30 characters is accepted from 23ai on; go-ora
// does not announce support for it, which the dialer repairs. The script
// creates the monitoring user with such a password, and this guards the
// fix without depending on that: it only asserts the login when the
// configured password is long.
func TestIntegration_LongPasswordLogin(t *testing.T) {
	tg := targetFromEnv(t)
	if len(tg.password) <= 30 {
		t.Skipf("the test password has %d characters; set a password over 30 characters to cover long-password logins", len(tg.password))
	}
	op := startProbe(t, tg)
	if up := one(t, collect(t, op), "senhub.db.up"); up.Value != 1 {
		t.Fatalf("senhub.db.up = %v with a %d-character password", up.Value, len(tg.password))
	}
}

func TestIntegration_WrongPasswordIsAMeasurement(t *testing.T) {
	tg := targetFromEnv(t)
	tg.password += "-wrong"
	op := startProbe(t, tg)
	if up := one(t, collect(t, op), "senhub.db.up"); up.Value != 0 {
		t.Fatalf("senhub.db.up = %v with a wrong password, want 0", up.Value)
	}
}
