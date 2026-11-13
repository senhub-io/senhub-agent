// Package unifi implements the free unifi probe: a Ubiquiti UniFi
// Controller monitor that talks the controller's REST API over stdlib
// HTTP (cookie session auth, no external dependency).
//
// One cycle logs in (POST /api/login), then reads three controller
// endpoints for a site:
//
//   - GET /api/s/{site}/stat/health  → per-subsystem health (WAN throughput)
//   - GET /api/s/{site}/stat/device  → APs / switches / gateways inventory
//   - GET /api/s/{site}/stat/sta     → connected clients
//
// It emits a controller-reachability gauge plus device, client, network
// and per-device metrics. A failing cycle is a measurement (up=0), never
// a collection error — the always-emit-up contract shared with the other
// free active-check probes.
package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier (license JWT claims,
// transformer file path, DiscriminantTagsRegistry key).
const ProbeType = "unifi"

const (
	defaultEndpoint = "https://localhost:8443"
	defaultSite     = "default"
	defaultInterval = 60 * time.Second
	defaultTimeout  = 15 * time.Second
)

type unifiConfig struct {
	Endpoint  string
	Username  string
	Password  string
	Site      string
	VerifyTLS bool
	Interval  time.Duration
	Timeout   time.Duration
}

type unifiProbe struct {
	*types.BaseProbe
	cfg          unifiConfig
	moduleLogger *logger.ModuleLogger
	client       *http.Client

	entitySource *unifiEntitySource

	identityOnce sync.Once
}

// unifiHTTPStatusError preserves the HTTP status code so the check result
// can be classified with dbcommon.ClassifyHTTPStatus rather than generic
// error matching.
type unifiHTTPStatusError struct {
	path string // empty for login
	code int
}

func (e *unifiHTTPStatusError) Error() string {
	if e.path != "" {
		return fmt.Sprintf("%s returned status %d", e.path, e.code)
	}
	return fmt.Sprintf("login returned status %d", e.code)
}

// NewUnifiProbe builds a unifi probe from its raw params block.
func NewUnifiProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.unifi")

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}

	probe := &unifiProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Jar:     jar,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: !cfg.VerifyTLS, // #nosec G402 - verify_tls defaults to true; operator must set verify_tls: false to skip verification
				},
			},
		},
		entitySource: newEntitySource(cfg.Endpoint),
	}
	probe.SetProbeType(ProbeType)
	probe.SetEntitySource(probe.entitySource)
	return probe, nil
}

func parseConfig(raw map[string]interface{}) (unifiConfig, error) {
	cfg := unifiConfig{
		Endpoint:  defaultEndpoint,
		Site:      defaultSite,
		Interval:  defaultInterval,
		Timeout:   defaultTimeout,
		VerifyTLS: true, // secure by default; set verify_tls: false to accept self-signed certs in a lab
	}

	if v, ok := raw["endpoint"].(string); ok && v != "" {
		cfg.Endpoint = v
	}
	if v, ok := raw["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := raw["password"].(string); ok {
		cfg.Password = v
	}
	if v, ok := raw["site"].(string); ok && v != "" {
		cfg.Site = v
	}
	if v, ok := raw["verify_tls"].(bool); ok {
		cfg.VerifyTLS = v
	}
	if v, ok := types.IntParam(raw, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(raw, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}

	if cfg.Username == "" || cfg.Password == "" {
		return cfg, fmt.Errorf("unifi requires both username and password")
	}
	return cfg, nil
}

func (p *unifiProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *unifiProbe) ShouldStart() bool          { return true }
func (p *unifiProbe) GetInterval() time.Duration { return p.cfg.Interval }

// OnStart opens no connection: the session is established per cycle in
// Collect.
func (p *unifiProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Str("site", p.cfg.Site).
		Msg("Starting unifi probe")
	return nil
}

func (p *unifiProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect collects without a cancellation signal; the poller calls
// CollectContext.
func (p *unifiProbe) Collect() ([]data_store.DataPoint, error) {
	return p.CollectContext(context.Background())
}

// CollectContext runs one cycle: login, fetch the three endpoints, build the
// metric set. A login or fetch failure surfaces as senhub.unifi.up=0,
// not a collection error.
func (p *unifiProbe) CollectContext(ctx context.Context) ([]data_store.DataPoint, error) {
	now := time.Now()

	var points []data_store.DataPoint
	emitUp := func(up float64, err error, baseTags []tags.Tag, controllerTags []tags.Tag) []data_store.DataPoint {
		upPoint := data_store.DataPoint{Name: "senhub.unifi.up", Value: up, Timestamp: now, Tags: controllerTags}
		checkTags := append([]tags.Tag(nil), baseTags...)
		checkTags = append(checkTags, tags.Tag{Key: "metric_type", Value: "status"})
		checkPoint := data_store.DataPoint{
			Name:      dbcommon.CheckResultMetric,
			Value:     float64(p.classifyError(err)),
			Timestamp: now,
			Tags:      checkTags,
		}
		pts := append([]data_store.DataPoint{upPoint, checkPoint}, points...)
		return p.BaseProbe.EnrichDataPointsWithProbeName(pts, p.GetName())
	}

	if err := p.login(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("unifi collection interrupted: %w", ctxErr)
		}
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).Msg("unifi login failed")
		p.entitySource.markReachable(false)
		baseTags, controllerTags := p.collectionTags()
		return emitUp(0, err, baseTags, controllerTags), nil
	}

	health, err := p.fetchHealth(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("unifi collection interrupted: %w", ctxErr)
		}
		p.moduleLogger.Warn().Err(err).Msg("unifi health fetch failed")
		p.entitySource.markReachable(false)
		baseTags, controllerTags := p.collectionTags()
		return emitUp(0, err, baseTags, controllerTags), nil
	}
	devices, err := p.fetchDevices(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("unifi collection interrupted: %w", ctxErr)
		}
		p.moduleLogger.Warn().Err(err).Msg("unifi device fetch failed")
		p.entitySource.markReachable(false)
		baseTags, controllerTags := p.collectionTags()
		return emitUp(0, err, baseTags, controllerTags), nil
	}
	clients, err := p.fetchClients(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("unifi collection interrupted: %w", ctxErr)
		}
		p.moduleLogger.Warn().Err(err).Msg("unifi client fetch failed")
		p.entitySource.markReachable(false)
		baseTags, controllerTags := p.collectionTags()
		return emitUp(0, err, baseTags, controllerTags), nil
	}

	p.resolveControllerIdentity(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("unifi collection interrupted: %w", ctxErr)
	}
	p.entitySource.markReachable(true)
	baseTags, controllerTags := p.collectionTags()
	points = append(points, p.buildNetworkPoints(health, controllerTags, now)...)
	points = append(points, p.buildDevicePoints(devices, baseTags, now)...)
	points = append(points, p.buildClientPoints(clients, controllerTags, now)...)

	return emitUp(1, nil, baseTags, controllerTags), nil
}

// collectionTags builds the probe-wide base tags after the entity source has
// been marked observed for this cycle, so the identity key (when present) is
// exactly the ID the entity source will emit.
func (p *unifiProbe) collectionTags() (baseTags []tags.Tag, controllerTags []tags.Tag) {
	baseTags = []tags.Tag{
		{Key: "endpoint", Value: p.cfg.Endpoint},
		{Key: "site", Value: p.cfg.Site},
	}
	if id := p.entitySource.ServiceInstanceID(); id != "" {
		baseTags = append(baseTags, tags.Tag{Key: idKeyServiceInstanceID, Value: id})
	}
	controllerTags = append(append([]tags.Tag(nil), baseTags...), tags.Tag{Key: "metric_type", Value: "controller"})
	return baseTags, controllerTags
}

func (p *unifiProbe) classifyError(err error) dbcommon.CheckResult {
	if err == nil {
		return dbcommon.CheckOK
	}
	var statusErr *unifiHTTPStatusError
	if errors.As(err, &statusErr) {
		return dbcommon.ClassifyHTTPStatus(statusErr.code)
	}
	return dbcommon.ClassifyError(err)
}

// sysinfoEnvelope is the subset of stat/sysinfo that identifies the
// controller. anonymous_controller_id is the UUID the controller generates for
// itself; it is readable by a read-only administrator.
type sysinfoEnvelope struct {
	Data []struct {
		ControllerID string `json:"anonymous_controller_id"`
	} `json:"data"`
}

// resolveControllerIdentity reads the controller's own UUID until it succeeds.
// A failure is not an error of the cycle: the entity then falls back to the
// host-scoped form for a local controller, or is not emitted for a remote one,
// and that is explained once.
func (p *unifiProbe) resolveControllerIdentity(ctx context.Context) {
	var env sysinfoEnvelope
	err := p.getJSON(ctx, p.sitePath("/stat/sysinfo"), &env)
	if err == nil && len(env.Data) > 0 && env.Data[0].ControllerID != "" {
		p.entitySource.setControllerID(env.Data[0].ControllerID)
		return
	}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.moduleLogger.Debug().Err(err).Msg("unifi controller UUID not readable")
	}
	p.identityOnce.Do(func() {
		if p.entitySource.isLocalController() {
			p.moduleLogger.Info().Msg("unifi: the controller UUID is not readable by this account; the controller entity is keyed on this host (unifi@<host.id>)")
			return
		}
		p.moduleLogger.Info().Str("endpoint", p.cfg.Endpoint).Msg("unifi: the controller is remote and its UUID is not readable by this account; no controller entity is emitted because there is no identifiable key (use an account that can read stat/sysinfo)")
	})
}

// login posts credentials and lets the cookie jar capture the session.
func (p *unifiProbe) login(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{
		"username": p.cfg.Username,
		"password": p.cfg.Password,
	})
	if err != nil {
		return fmt.Errorf("encoding login body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.Endpoint+"/api/login", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return &unifiHTTPStatusError{code: resp.StatusCode}
	}
	return nil
}

// getJSON issues an authenticated GET and decodes the controller's
// {"data": [...]} envelope into out.
func (p *unifiProbe) getJSON(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.Endpoint+path, nil)
	if err != nil {
		return fmt.Errorf("building request %s: %w", path, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return &unifiHTTPStatusError{path: path, code: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}

func (p *unifiProbe) sitePath(suffix string) string {
	return "/api/s/" + p.cfg.Site + suffix
}

// healthEnvelope is the subset of stat/health consumed for WAN throughput.
type healthEnvelope struct {
	Data []struct {
		Subsystem string  `json:"subsystem"`
		TxBytesR  float64 `json:"tx_bytes-r"`
		RxBytesR  float64 `json:"rx_bytes-r"`
	} `json:"data"`
}

func (p *unifiProbe) fetchHealth(ctx context.Context) (healthEnvelope, error) {
	var env healthEnvelope
	err := p.getJSON(ctx, p.sitePath("/stat/health"), &env)
	return env, err
}

// deviceEnvelope is the subset of stat/device consumed for inventory and
// per-device health.
type deviceEnvelope struct {
	Data []deviceRow `json:"data"`
}

type deviceRow struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	State   int     `json:"state"`
	Adopted bool    `json:"adopted"`
	NumSta  float64 `json:"num_sta"`
	// Score is the controller's satisfaction (0-100), null when it has
	// none (a device that is not connected, or too few clients).
	Score    *float64 `json:"satisfaction"`
	SysStats struct {
		CPU string `json:"cpu"`
		Mem string `json:"mem"`
	} `json:"system-stats"`
}

func (p *unifiProbe) fetchDevices(ctx context.Context) (deviceEnvelope, error) {
	var env deviceEnvelope
	err := p.getJSON(ctx, p.sitePath("/stat/device"), &env)
	return env, err
}

// clientEnvelope is the subset of stat/sta consumed for client counts.
type clientEnvelope struct {
	Data []struct {
		IsWired bool `json:"is_wired"`
	} `json:"data"`
}

func (p *unifiProbe) fetchClients(ctx context.Context) (clientEnvelope, error) {
	var env clientEnvelope
	err := p.getJSON(ctx, p.sitePath("/stat/sta"), &env)
	return env, err
}

// buildNetworkPoints emits WAN throughput counters from the wan subsystem
// of stat/health. Both directions are emitted as a single metric name
// (unifi.network.io) discriminated by the OTel attribute
// network.io.direction (values: "transmit" / "receive").
func (p *unifiProbe) buildNetworkPoints(health healthEnvelope, baseTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, h := range health.Data {
		if h.Subsystem != "wan" {
			continue
		}
		txTags := append(append([]tags.Tag(nil), baseTags...), tags.Tag{Key: "direction", Value: "transmit"})
		rxTags := append(append([]tags.Tag(nil), baseTags...), tags.Tag{Key: "direction", Value: "receive"})
		points = append(points,
			data_store.DataPoint{Name: "unifi.network.io", Value: float64(h.TxBytesR), Timestamp: ts, Tags: txTags},
			data_store.DataPoint{Name: "unifi.network.io", Value: float64(h.RxBytesR), Timestamp: ts, Tags: rxTags},
		)
	}
	return points
}

// buildDevicePoints emits the per-type inventory rollups and the
// per-device cpu/memory/satisfaction gauges.
func (p *unifiProbe) buildDevicePoints(devices deviceEnvelope, baseTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	type counts struct{ total, adopted, disconnected int }
	byType := map[string]*counts{}

	var points []data_store.DataPoint
	for _, d := range devices.Data {
		dt := shortType(d.Type)
		c := byType[dt]
		if c == nil {
			c = &counts{}
			byType[dt] = c
		}
		c.total++
		if d.Adopted {
			c.adopted++
		}
		if d.State != 1 { // state 1 = connected
			c.disconnected++
		}

		devTags := append([]tags.Tag(nil), baseTags...)
		devTags = append(devTags,
			tags.Tag{Key: "device_name", Value: deviceName(d)},
			tags.Tag{Key: "device_type", Value: dt},
			tags.Tag{Key: "metric_type", Value: "device"},
		)
		if cpu, ok := parseFloat(d.SysStats.CPU); ok {
			points = append(points, data_store.DataPoint{Name: "unifi.device.cpu", Value: float64(cpu), Timestamp: ts, Tags: devTags})
		}
		if mem, ok := parseFloat(d.SysStats.Mem); ok {
			points = append(points, data_store.DataPoint{Name: "unifi.device.memory", Value: float64(mem), Timestamp: ts, Tags: devTags})
		}
		// An access point that is not connected has no clients to count and
		// no satisfaction to report: the controller returns null for it,
		// and a 0 would read as the worst experience possible.
		if dt == "uap" && d.State == 1 {
			apTags := append([]tags.Tag(nil), baseTags...)
			apTags = append(apTags,
				tags.Tag{Key: "device_name", Value: deviceName(d)},
				tags.Tag{Key: "metric_type", Value: "access_point"},
			)
			points = append(points,
				data_store.DataPoint{Name: "unifi.ap.clients", Value: float64(d.NumSta), Timestamp: ts, Tags: apTags},
			)
			if d.Score != nil {
				points = append(points,
					data_store.DataPoint{Name: "unifi.ap.satisfaction", Value: *d.Score / 100, Timestamp: ts, Tags: apTags},
				)
			}
		}
	}

	for dt, c := range byType {
		typeTags := append([]tags.Tag(nil), baseTags...)
		typeTags = append(typeTags,
			tags.Tag{Key: "device_type", Value: dt},
			tags.Tag{Key: "metric_type", Value: "inventory"},
		)
		points = append(points,
			data_store.DataPoint{Name: "unifi.devices.total", Value: float64(c.total), Timestamp: ts, Tags: typeTags},
			data_store.DataPoint{Name: "unifi.devices.adopted", Value: float64(c.adopted), Timestamp: ts, Tags: typeTags},
			data_store.DataPoint{Name: "unifi.devices.disconnected", Value: float64(c.disconnected), Timestamp: ts, Tags: typeTags},
		)
	}
	return points
}

// buildClientPoints emits the total and wifi-only client counts.
func (p *unifiProbe) buildClientPoints(clients clientEnvelope, baseTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	total := len(clients.Data)
	wifi := 0
	for _, c := range clients.Data {
		if !c.IsWired {
			wifi++
		}
	}
	return []data_store.DataPoint{
		{Name: "unifi.clients.total", Value: float64(total), Timestamp: ts, Tags: baseTags},
		{Name: "unifi.clients.wifi", Value: float64(wifi), Timestamp: ts, Tags: baseTags},
	}
}

// shortType maps the controller's device type string to the stable short
// label. The controller already uses uap/usw/ugw; anything else is kept
// verbatim, with an empty type bucketed under "other".
func shortType(t string) string {
	switch t {
	case "uap", "usw", "ugw":
		return t
	case "":
		return "other"
	default:
		return t
	}
}

// deviceName falls back to the device type when the controller has no
// friendly name yet (a freshly-adopted device), so the device tag is
// never empty.
func deviceName(d deviceRow) string {
	if d.Name != "" {
		return d.Name
	}
	return shortType(d.Type)
}

// parseFloat parses the controller's stringified cpu/mem percentages.
func parseFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// drain consumes and closes a response body so the connection can be
// reused by keep-alive.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
