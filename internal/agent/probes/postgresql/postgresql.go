// Package postgresql provides a probe for PostgreSQL instances
// (community, RDS, Aurora, Cloud SQL, Azure Flexible, Supabase).
// See docs/developer-guide/database-probes/DESIGN.md and METRICS.md.
package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	// pgx exposes a database/sql driver under "pgx" once this
	// blank import registers it. The probe uses database/sql
	// directly so the surface stays similar to the mysql probe.
	_ "github.com/jackc/pgx/v5/stdlib"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

type postgresqlProbe struct {
	*types.BaseProbe

	logger   *logger.ModuleLogger
	cfg      *probeConfig
	interval time.Duration

	db *sql.DB

	// versionString captured at OnStart. versionNum is the integer
	// form used by version-aware queries (see DESIGN §5.4) — it
	// corresponds to PG's server_version_num, e.g. 160003 for
	// 16.3 — making `>= 130000` comparisons cheap.
	versionString string
	versionNum    int
	environment   dbcommon.Environment

	ctx        context.Context
	cancelFunc context.CancelFunc
}

func NewPostgreSQLProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.postgresql")
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &postgresqlProbe{
		BaseProbe:  &types.BaseProbe{},
		logger:     moduleLogger,
		cfg:        cfg,
		interval:   time.Duration(cfg.Interval) * time.Second,
		ctx:        ctx,
		cancelFunc: cancel,
	}, nil
}

func (p *postgresqlProbe) ShouldStart() bool         { return true }
func (p *postgresqlProbe) GetInterval() time.Duration { return p.interval }

func (p *postgresqlProbe) OnStart(quitChannel chan struct{}) error {
	dsn := buildDSN(p.cfg)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("postgresql probe: open driver: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	pingCtx, cancel := context.WithTimeout(p.ctx, time.Duration(p.cfg.Timeout)*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return fmt.Errorf("postgresql probe: ping %s:%d: %w", p.cfg.Host, p.cfg.Port, err)
	}

	// Two version capture queries: the human-readable banner for
	// logging + environment detect, and the integer
	// server_version_num used by version-aware code paths.
	var versionString string
	var versionNum int
	if err := db.QueryRowContext(pingCtx, "SELECT version()").Scan(&versionString); err != nil {
		_ = db.Close()
		return fmt.Errorf("postgresql probe: SELECT version(): %w", err)
	}
	if err := db.QueryRowContext(pingCtx, "SHOW server_version_num").Scan(&versionNum); err != nil {
		// Non-fatal — we lose the integer comparison but the
		// probe can still run. Some forks may not expose this
		// GUC; default to 0 and let version-aware branches fall
		// back to their safe path.
		p.logger.Warn().Err(err).Msg("server_version_num unavailable; version-aware branches will use safe defaults")
	}

	p.db = db
	p.versionString = versionString
	p.versionNum = versionNum
	p.environment = dbcommon.DetectEnvironment(versionString)

	p.logger.Info().
		Str("host", p.cfg.Host).
		Int("port", p.cfg.Port).
		Str("version", p.versionString).
		Int("version_num", p.versionNum).
		Str("environment", string(p.environment)).
		Msg("postgresql probe connected")

	return nil
}

func (p *postgresqlProbe) OnShutdown(_ context.Context) error {
	p.cancelFunc()
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

func (p *postgresqlProbe) Collect() ([]datapoint.DataPoint, error) {
	if p.db == nil {
		return nil, fmt.Errorf("postgresql probe: not initialised (Collect before OnStart)")
	}
	timestamp := time.Now()

	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(p.cfg.Timeout)*time.Second)
	defer cancel()

	if err := p.db.PingContext(ctx); err != nil {
		return []datapoint.DataPoint{p.buildUpDatapoint(timestamp, false)}, fmt.Errorf("postgresql probe: ping: %w", err)
	}

	role, err := p.detectRole(ctx)
	if err != nil {
		p.logger.Warn().Err(err).Msg("role detection failed; defaulting to standalone")
		role = dbcommon.RoleStandalone
	}

	var points []datapoint.DataPoint
	points = append(points, p.buildUpDatapoint(timestamp, true))
	points = append(points, p.buildOverviewMetrics(ctx, timestamp, role)...)
	points = append(points, p.buildConnectionsMetrics(ctx, timestamp)...)
	points = append(points, p.buildThroughputMetrics(ctx, timestamp)...)
	points = append(points, p.buildReplicationMetrics(ctx, timestamp, role)...)
	points = append(points, p.buildCacheMetrics(ctx, timestamp)...)
	points = append(points, p.buildLocksMetrics(ctx, timestamp)...)
	points = append(points, p.buildStorageMetrics(ctx, timestamp)...)

	return points, nil
}

// buildDSN assembles a libpq-style key=value connection string.
// pgx parses both URL and keyword form; the keyword form is easier
// to escape (passwords with spaces, special characters in hosts).
func buildDSN(cfg *probeConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "host=%s port=%d user=%s ", cfg.Host, cfg.Port, cfg.Username)
	if cfg.Password != "" {
		// libpq accepts single quotes around values that contain
		// spaces or '. Escape embedded single quotes with '\'.
		fmt.Fprintf(&b, "password='%s' ", strings.ReplaceAll(cfg.Password, `'`, `\'`))
	}
	fmt.Fprintf(&b, "dbname=%s ", cfg.Database)
	fmt.Fprintf(&b, "sslmode=%s ", cfg.SSLMode)
	if cfg.SSLRootCert != "" {
		fmt.Fprintf(&b, "sslrootcert=%s ", cfg.SSLRootCert)
	}
	fmt.Fprintf(&b, "connect_timeout=%d", cfg.Timeout)
	return b.String()
}
