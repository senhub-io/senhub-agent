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
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	mongodrv "go.mongodb.org/mongo-driver/mongo"
	mongooptions "go.mongodb.org/mongo-driver/mongo/options"

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

	// fetchReplSetID is the function used to probe replSetGetStatus. Overridable
	// in tests. Returns ("mongodb:<setName>/<selfMember>", nil) on a replica set,
	// ("", nil) when the command is unavailable (standalone), or ("", err) on
	// transport failure. A ("", nil) result triggers the host:port fallback.
	fetchReplSetID func(ctx context.Context) (string, error)

	// entitySrc feeds the Toise topology inventory (db entity).
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
	probe.entitySrc = newMongodbEntitySource(instHost, instPort, cfg.InstanceName)
	// fetchReplSetID is wired after construction so it can close over probe.client
	// (which is nil until OnStart). The probe pointer is stable.
	probe.fetchReplSetID = probe.defaultFetchReplSetID
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
		// Resolve the db entity identity on the first successful collect:
		//  - if already pinned (instance_name or prior replSetGetStatus), skip.
		//  - try replSetGetStatus to get a stable tech id.
		//  - fall back to host:port when replSetGetStatus is unavailable (standalone).
		p.maybeResolveEntityID()
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

// maybeResolveEntityID pins the db.instance.id on the first call after a
// successful serverStatus. Skips the replSetGetStatus round-trip when the id
// is already pinned (instance_name set at construction, or prior successful
// call). Subsequent calls after pinning are no-ops.
func (p *mongoDBProbe) maybeResolveEntityID() {
	if p.entitySrc.isPinned() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.Timeout)
	defer cancel()

	techID, err := p.fetchReplSetID(ctx)
	if err != nil {
		// Transport failure: don't pin yet; retry next cycle.
		p.moduleLogger.Debug().Err(err).Msg("replSetGetStatus transport error; will retry next cycle")
		return
	}
	if techID != "" {
		// Replica set identity resolved.
		p.entitySrc.pinTechID(techID)
		return
	}
	// techID=="" with err==nil: standalone MongoDB (no replSet).
	// host:port is the only available id; pin it immediately.
	p.entitySrc.pinHostPort()
}

// defaultFetchReplSetID issues replSetGetStatus against the live client.
// Returns ("mongodb:<setName>/<selfMember>", nil) on a replica set,
// ("", nil) when the command is not applicable (standalone, no-auth error
// with code NotYetInitialized or errTypeBadValue), or ("", err) on transport
// failure.
func (p *mongoDBProbe) defaultFetchReplSetID(ctx context.Context) (string, error) {
	result := bson.M{}
	err := p.client.Database("admin").RunCommand(ctx, bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&result)
	if err != nil {
		// MongoDB returns a command error (code 76 NoReplicationEnabled or
		// code 94 NotYetInitialized) when the server is a standalone.
		// These are expected "not a replica set" responses, not transport errors.
		// The MongoDB Go driver wraps command errors as mongo.CommandError.
		if isNotReplicaSetError(err) {
			return "", nil
		}
		return "", err
	}

	setName, _ := result["set"].(string)
	if setName == "" {
		return "", nil
	}

	// Find the member marked self:true to get its canonical name.
	selfMember := ""
	if members, ok := result["members"].(bson.A); ok {
		for _, m := range members {
			mem, ok := m.(bson.M)
			if !ok {
				continue
			}
			if isSelf, _ := mem["self"].(bool); isSelf {
				selfMember, _ = mem["name"].(string)
				break
			}
		}
	}

	if selfMember != "" {
		return "mongodb:" + setName + "/" + selfMember, nil
	}
	return "mongodb:" + setName, nil
}

// isNotReplicaSetError reports whether err is a MongoDB command error
// indicating that the server is not part of a replica set (standalone).
// These are expected responses, not transport failures.
// Code 76 = NoReplicationEnabled (standalone), Code 94 = NotYetInitialized.
func isNotReplicaSetError(err error) bool {
	var ce mongodrv.CommandError
	if errors.As(err, &ce) {
		return ce.Code == 76 || ce.Code == 94
	}
	return false
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
				key    string
				opType string
				metric string
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

// hostPortFromURI extracts the host and port from a MongoDB URI such as
// "mongodb://user:pass@host:27017/dbname". Falls back to "localhost" / 27017
// when the URI cannot be parsed or has no explicit port.
func hostPortFromURI(uri string) (host string, port int64) {
	host = "localhost"
	port = 27017

	u, err := url.Parse(uri)
	if err != nil || u.Host == "" {
		return
	}
	if h := u.Hostname(); h != "" {
		host = h
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.ParseInt(p, 10, 64); err == nil {
			port = n
		}
	}
	return
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
