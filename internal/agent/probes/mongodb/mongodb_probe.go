// Package mongodb implements the free mongodb probe: serverStatus + per-database
// dbStats collection via the official MongoDB Go driver (#TODO: issue number).
//
// Metrics follow the OTel contrib mongodbreceiver canonical names (parité
// opentelemetry-collector-contrib/receiver/mongodbreceiver). Metrics for which
// no contrib equivalent exists are extended under senhub.mongodb.*.
//
// Connection model: the probe opens a mongo.Client on OnStart, pings the
// server each Collect cycle to detect restarts, and closes cleanly on
// OnShutdown. A connection failure emits senhub.mongodb.up=0 and returns
// nil (observable outage, never a collection error).
package mongodb

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	mongooptions "go.mongodb.org/mongo-driver/mongo/options"

	mongodrv "go.mongodb.org/mongo-driver/mongo"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// metric_type values — used as sensor-builder family chips.
const (
	metricTypeStatus   = "status"
	metricTypeConns    = "connections"
	metricTypeNetwork  = "network"
	metricTypeOps      = "operations"
	metricTypeMemory   = "memory"
	metricTypeDocs     = "documents"
	metricTypeCache    = "cache"
	metricTypeLocks    = "locks"
	metricTypeDatabase = "database"
)

// mongoDBProbe polls one MongoDB instance per cycle.
type mongoDBProbe struct {
	*types.BaseProbe
	cfg          *config
	instance     string
	moduleLogger *logger.ModuleLogger

	// client is the long-lived connection pool (nil until OnStart).
	client *mongodrv.Client

	// connectClient is the factory used to create a mongo.Client. Overridable
	// in tests so no real MongoDB is required.
	connectClient func(ctx context.Context, uri string, direct bool, timeout time.Duration) (*mongodrv.Client, error)

	// entitySrc feeds the Toise topology inventory (db.mongodb entity).
	entitySrc  *mongodbEntitySource
	unregister func()
}

// NewMongoDBProbe builds a mongodb probe from its raw params block.
func NewMongoDBProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("mongodb: %w", err)
	}

	// Derive a credential-free instance string once at construction.
	// cfg.URI may contain user:password — never store or emit it.
	instHost, instPort := hostPortFromURI(cfg.URI)
	instance := "mongodb://" + instHost + ":" + strconv.FormatInt(instPort, 10)

	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.mongodb")
	moduleLogger.Debug().
		Str("instance", instance).
		Bool("direct_connection", cfg.DirectConnection).
		Dur("timeout", cfg.Timeout).
		Msg("Creating new MongoDB probe")

	probe := &mongoDBProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     instance,
		moduleLogger: moduleLogger,
		connectClient: func(ctx context.Context, uri string, direct bool, timeout time.Duration) (*mongodrv.Client, error) {
			opts := mongooptions.Client().
				ApplyURI(uri).
				SetDirect(direct).
				SetServerSelectionTimeout(timeout).
				SetConnectTimeout(timeout)
			return mongodrv.Connect(ctx, opts)
		},
	}
	probe.SetProbeType(probeType)
	probe.entitySrc = newMongodbEntitySource(cfg.URI)
	return probe, nil
}

// ShouldStart always returns true — the probe runs on every agent.
func (p *mongoDBProbe) ShouldStart() bool { return true }

// GetInterval returns the configured collection interval.
func (p *mongoDBProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart opens the MongoDB client. The client uses a connection pool internally;
// no connection is actually established until the first command is issued.
func (p *mongoDBProbe) OnStart(_ chan struct{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	client, err := p.connectClient(ctx, p.cfg.URI, p.cfg.DirectConnection, p.cfg.Timeout)
	if err != nil {
		return fmt.Errorf("mongodb: connecting to %s: %w", p.instance, err)
	}
	p.client = client
	p.unregister = entity.RegisterSource(p.entitySrc)
	p.moduleLogger.Info().Str("instance", p.instance).Msg("MongoDB probe started")
	return nil
}

// OnShutdown closes the MongoDB client cleanly.
func (p *mongoDBProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	if p.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.client.Disconnect(ctx); err != nil {
		p.moduleLogger.Warn().Err(err).Msg("MongoDB client disconnect error")
	}
	return nil
}

// Collect runs one poll cycle. A connection or command failure emits
// senhub.mongodb.up=0 and returns nil (observable outage).
func (p *mongoDBProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()

	var points []data_store.DataPoint
	up := float32(1)

	status, err := p.runServerStatus()
	if err != nil {
		up = 0
		p.entitySrc.setReachable(false, "")
		p.moduleLogger.Warn().Err(err).Str("instance", p.instance).Msg("MongoDB serverStatus failed")
	} else {
		version, _ := status["version"].(string)
		p.entitySrc.setReachable(true, version)
		points = append(points, p.buildServerStatusPoints(status, now)...)

		dbPoints, dbErr := p.collectDatabaseStats(now)
		if dbErr != nil {
			p.moduleLogger.Warn().Err(dbErr).Msg("MongoDB dbStats collection failed")
		} else {
			points = append(points, dbPoints...)
		}
	}

	points = append(points, data_store.DataPoint{
		Name:      "senhub.mongodb.up",
		Value:     up,
		Timestamp: now,
		Tags:      p.baseTags(metricTypeStatus),
	})

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// serverStatus helpers
// ─────────────────────────────────────────────────────────────────────────────

func (p *mongoDBProbe) runServerStatus() (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	result := bson.M{}
	if err := p.client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&result); err != nil {
		return nil, fmt.Errorf("serverStatus: %w", err)
	}
	return result, nil
}

func (p *mongoDBProbe) buildServerStatusPoints(status bson.M, now time.Time) []data_store.DataPoint {
	var pts []data_store.DataPoint

	// uptime — uptimeMillis / 1000
	if ms, ok := floatFrom(status, "uptimeMillis"); ok {
		pts = append(pts, data_store.DataPoint{
			Name:      "mongodb.uptime",
			Value:     float32(ms / 1000),
			Timestamp: now,
			Tags:      p.baseTags(metricTypeStatus),
		})
	}

	// connections
	if conns, ok := status["connections"].(bson.M); ok {
		for _, entry := range []struct {
			key    string
			metric string
		}{
			{"active", "mongodb.connections.active"},
			{"available", "mongodb.connections.available"},
			{"current", "mongodb.connections.current"},
		} {
			if v, ok := floatFrom(conns, entry.key); ok {
				pts = append(pts, data_store.DataPoint{
					Name:      entry.metric,
					Value:     float32(v),
					Timestamp: now,
					Tags:      p.baseTags(metricTypeConns),
				})
			}
		}
	}

	// network
	if net, ok := status["network"].(bson.M); ok {
		for _, entry := range []struct {
			key    string
			metric string
		}{
			{"bytesIn", "mongodb.network.bytes.in"},
			{"bytesOut", "mongodb.network.bytes.out"},
			{"numRequests", "mongodb.network.requests"},
		} {
			if v, ok := floatFrom(net, entry.key); ok {
				pts = append(pts, data_store.DataPoint{
					Name:      entry.metric,
					Value:     float32(v),
					Timestamp: now,
					Tags:      p.baseTags(metricTypeNetwork),
				})
			}
		}
	}

	// opcounters — one series per operation
	if ops, ok := status["opcounters"].(bson.M); ok {
		for _, op := range []string{"insert", "query", "update", "delete", "getmore", "command"} {
			if v, ok := floatFrom(ops, op); ok {
				pts = append(pts, data_store.DataPoint{
					Name:      "mongodb.operations.count",
					Value:     float32(v),
					Timestamp: now,
					Tags:      append(p.baseTags(metricTypeOps), tags.Tag{Key: "operation", Value: op}),
				})
			}
		}
	}

	// memory — resident + virtual in bytes (mem values are in MB)
	if mem, ok := status["mem"].(bson.M); ok {
		for _, entry := range []struct {
			key    string
			memTyp string
		}{
			{"resident", "resident"},
			{"virtual", "virtual"},
		} {
			if v, ok := floatFrom(mem, entry.key); ok {
				pts = append(pts, data_store.DataPoint{
					Name:      "mongodb.memory.usage",
					Value:     float32(v * 1024 * 1024),
					Timestamp: now,
					Tags:      append(p.baseTags(metricTypeMemory), tags.Tag{Key: "type", Value: entry.memTyp}),
				})
			}
		}
	}

	// document operations (metrics.document.*)
	if metrics, ok := status["metrics"].(bson.M); ok {
		if docOps, ok := metrics["document"].(bson.M); ok {
			for _, op := range []string{"deleted", "inserted", "returned", "updated"} {
				if v, ok := floatFrom(docOps, op); ok {
					pts = append(pts, data_store.DataPoint{
						Name:      "mongodb.document.operations",
						Value:     float32(v),
						Timestamp: now,
						Tags:      append(p.baseTags(metricTypeDocs), tags.Tag{Key: "operation", Value: op}),
					})
				}
			}
		}
	}

	// wiredTiger cache operations
	if wt, ok := status["wiredTiger"].(bson.M); ok {
		if cache, ok := wt["cache"].(bson.M); ok {
			for _, entry := range []struct {
				key     string
				opType  string
				metric  string
			}{
				{"pages read into cache", "read", "mongodb.cache.operations"},
				{"pages written from cache", "write", "mongodb.cache.operations"},
			} {
				if v, ok := floatFrom(cache, entry.key); ok {
					pts = append(pts, data_store.DataPoint{
						Name:      entry.metric,
						Value:     float32(v),
						Timestamp: now,
						Tags:      append(p.baseTags(metricTypeCache), tags.Tag{Key: "type", Value: entry.opType}),
					})
				}
			}
		}
	}

	// globalLock — reader/writer queues
	if gl, ok := status["globalLock"].(bson.M); ok {
		if cq, ok := gl["currentQueue"].(bson.M); ok {
			if v, ok := floatFrom(cq, "readers"); ok {
				pts = append(pts, data_store.DataPoint{
					Name:      "mongodb.active.reads",
					Value:     float32(v),
					Timestamp: now,
					Tags:      p.baseTags(metricTypeLocks),
				})
			}
			if v, ok := floatFrom(cq, "writers"); ok {
				pts = append(pts, data_store.DataPoint{
					Name:      "mongodb.active.writes",
					Value:     float32(v),
					Timestamp: now,
					Tags:      p.baseTags(metricTypeLocks),
				})
			}
		}
	}

	return pts
}

// ─────────────────────────────────────────────────────────────────────────────
// per-database dbStats helpers
// ─────────────────────────────────────────────────────────────────────────────

// skipDatabases lists internal MongoDB system databases excluded from dbStats.
var skipDatabases = map[string]bool{
	"admin":  true,
	"local":  true,
	"config": true,
}

func (p *mongoDBProbe) collectDatabaseStats(now time.Time) ([]data_store.DataPoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	dbNames, err := p.client.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("ListDatabaseNames: %w", err)
	}

	var pts []data_store.DataPoint
	for _, dbName := range dbNames {
		if skipDatabases[dbName] {
			continue
		}

		dbPts, err := p.runDBStats(dbName, now)
		if err != nil {
			p.moduleLogger.Warn().Err(err).Str("database", dbName).Msg("dbStats failed")
			continue
		}
		pts = append(pts, dbPts...)
	}
	return pts, nil
}

func (p *mongoDBProbe) runDBStats(dbName string, now time.Time) ([]data_store.DataPoint, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	result := bson.M{}
	if err := p.client.Database(dbName).RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&result); err != nil {
		return nil, fmt.Errorf("dbStats(%s): %w", dbName, err)
	}

	dbTag := tags.Tag{Key: "database", Value: dbName}

	var pts []data_store.DataPoint
	for _, entry := range []struct {
		key    string
		metric string
	}{
		{"collections", "mongodb.collection.count"},
		{"dataSize", "mongodb.data.size"},
		{"indexes", "mongodb.index.count"},
		{"indexSize", "mongodb.index.size"},
		{"storageSize", "mongodb.storage.size"},
	} {
		if v, ok := floatFrom(result, entry.key); ok {
			pts = append(pts, data_store.DataPoint{
				Name:      entry.metric,
				Value:     float32(v),
				Timestamp: now,
				Tags:      append(p.baseTags(metricTypeDatabase), dbTag),
			})
		}
	}
	return pts, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// baseTags returns the common tags emitted on every datapoint.
func (p *mongoDBProbe) baseTags(metricType string) []tags.Tag {
	return []tags.Tag{
		{Key: "instance", Value: p.instance},
		{Key: "metric_type", Value: metricType},
	}
}

// floatFrom extracts a numeric value from a bson.M field as float64. bson.M
// values can be int32, int64, float64, or int (wire-format dependent), so we
// handle all four without panicking on an unexpected type.
func floatFrom(m bson.M, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}
