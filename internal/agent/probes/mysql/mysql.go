// Package mysql provides a probe for MySQL and MariaDB instances.
// See docs/developer-guide/database-probes/DESIGN.md for the design
// contract and docs/developer-guide/database-probes/METRICS.md for
// the metric catalog this probe implements.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// Imported for its side-effect of registering the "mysql" driver
	// with database/sql. The probe itself uses the database/sql API,
	// not the driver's own surface, to stay engine-agnostic where it
	// can.
	_ "github.com/go-sql-driver/mysql"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// mysqlProbe implements monitoring for MySQL and MariaDB.
type mysqlProbe struct {
	*types.BaseProbe

	logger   *logger.ModuleLogger
	cfg      *probeConfig
	interval time.Duration

	// db holds the single sql.DB used by the probe. database/sql
	// transparently maintains a connection pool internally; we
	// configure it down to one connection so the probe presents a
	// single, predictable session to the DBA.
	db *sql.DB

	// versionString is captured once at OnStart so DetectEnvironment
	// can run and so per-version SQL branches (e.g. version-aware
	// pg_stat_statements on the postgresql side) can decide at
	// collection time without re-querying.
	versionString string
	environment   dbcommon.Environment

	ctx        context.Context
	cancelFunc context.CancelFunc
}

// NewMySQLProbe is the registry entry point. Wired in
// internal/agent/probes/registry.go.
func NewMySQLProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.mysql")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &mysqlProbe{
		BaseProbe:  &types.BaseProbe{},
		logger:     moduleLogger,
		cfg:        cfg,
		interval:   time.Duration(cfg.Interval) * time.Second,
		ctx:        ctx,
		cancelFunc: cancel,
	}, nil
}

func (p *mysqlProbe) ShouldStart() bool { return true }

func (p *mysqlProbe) GetInterval() time.Duration { return p.interval }

// OnStart opens the database/sql handle, pings the server once to
// validate connectivity, captures the version + environment, and
// keeps the handle for subsequent Collect calls. Failure to connect
// returns an error so the framework reports the probe as unhealthy
// immediately rather than racing through scrape cycles that all
// fail.
func (p *mysqlProbe) OnStart(quitChannel chan struct{}) error {
	dsn := buildDSN(p.cfg)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("mysql probe: open driver: %w", err)
	}

	// One persistent connection — see DESIGN §2. database/sql's pool
	// is left enabled but capped at one so the probe shows up as a
	// single session in SHOW PROCESSLIST.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	pingCtx, cancel := context.WithTimeout(p.ctx, time.Duration(p.cfg.Timeout)*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return fmt.Errorf("mysql probe: ping %s:%d: %w", p.cfg.Host, p.cfg.Port, err)
	}

	// Capture the version string once. Two reasons:
	//   1. DetectEnvironment runs on a stable input.
	//   2. The user docs ask DBAs to verify which engine version
	//      the probe sees — a single Info log line at OnStart makes
	//      that obvious from the agent log without grepping every
	//      collect cycle.
	var versionString string
	if err := db.QueryRowContext(pingCtx, "SELECT VERSION()").Scan(&versionString); err != nil {
		_ = db.Close()
		return fmt.Errorf("mysql probe: SELECT VERSION(): %w", err)
	}

	p.db = db
	p.versionString = versionString
	p.environment = dbcommon.DetectEnvironment(versionString)

	p.logger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Str("version", p.versionString).
		Str("environment", string(p.environment)).
		Msg("mysql probe connected")

	return nil
}

// OnShutdown releases the connection.
func (p *mysqlProbe) OnShutdown(_ context.Context) error {
	p.cancelFunc()
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// Collect runs the per-cycle query fan-out and returns the resulting
// datapoints. Each family is its own helper in metrics.go so the
// implementation order tracks the design doc step-by-step and one
// helper failing does not poison the others.
func (p *mysqlProbe) Collect() ([]datapoint.DataPoint, error) {
	if p.db == nil {
		return nil, fmt.Errorf("mysql probe: not initialised (Collect before OnStart)")
	}

	timestamp := time.Now()

	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(p.cfg.Timeout)*time.Second)
	defer cancel()

	// Ping before each cycle so a server restart or wait_timeout
	// expiry is detected explicitly rather than appearing as a
	// torrent of "bad connection" errors from the per-family
	// helpers. The reconnect is automatic via database/sql.
	if err := p.db.PingContext(ctx); err != nil {
		// Emit the down datapoint then return — the rest of the
		// catalog cannot be queried.
		return []datapoint.DataPoint{
			p.buildUpDatapoint(timestamp, false),
		}, fmt.Errorf("mysql probe: ping: %w", err)
	}

	var points []datapoint.DataPoint

	// Order matters only in that the role detect feeds the
	// replication family. The other families are independent.
	role, err := p.detectRole(ctx)
	if err != nil {
		p.logger.Warn().Err(err).Msg("role detection failed; defaulting to standalone")
		role = dbcommon.RoleStandalone
	}

	points = append(points, p.buildUpDatapoint(timestamp, true))
	points = append(points, p.buildOverviewMetrics(ctx, timestamp, role)...)

	return points, nil
}

// buildDSN assembles the database/sql connection string from the
// validated config. Kept in mysql.go (not config.go) because it is
// driver-specific and not part of the probe's user contract.
func buildDSN(cfg *probeConfig) string {
	// mysql DSN: user:password@tcp(host:port)/dbname?param=value
	tls := "false"
	if cfg.TLSEnabled {
		tls = "true"
		if cfg.TLSSkipVerify {
			tls = "skip-verify"
		}
	}
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?timeout=%ds&readTimeout=%ds&writeTimeout=%ds&tls=%s&parseTime=true",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
		cfg.Timeout, cfg.Timeout, cfg.Timeout, tls,
	)
}
