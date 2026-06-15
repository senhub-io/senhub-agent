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
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
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
	unregister   func()
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
					InsecureSkipVerify: !cfg.VerifyTLS, // #nosec G402 - controllers ship a self-signed cert by default; verify_tls opts in
				},
			},
		},
		entitySource: newEntitySource(cfg.Endpoint),
	}
	probe.SetProbeType(ProbeType)
	return probe, nil
}

func parseConfig(raw map[string]interface{}) (unifiConfig, error) {
	cfg := unifiConfig{
		Endpoint: defaultEndpoint,
		Site:     defaultSite,
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
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
	if v, ok := raw["interval"].(int); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := raw["timeout"].(int); ok && v > 0 {
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

// OnStart registers the entity source so the monitored controller folds
// into the agent's entity snapshot. No connection is opened here: the
// session is established per cycle in Collect.
func (p *unifiProbe) OnStart(_ chan struct{}) error {
	p.unregister = entity.RegisterSource(p.entitySource)
	p.moduleLogger.Info().
		Str("endpoint", p.cfg.Endpoint).
		Str("site", p.cfg.Site).
		Msg("Starting unifi probe")
	return nil
}

func (p *unifiProbe) OnShutdown(_ context.Context) error {
	if p.unregister != nil {
		p.unregister()
	}
	p.client.CloseIdleConnections()
	return nil
}

// Collect runs one cycle: login, fetch the three endpoints, build the
// metric set. A login or fetch failure surfaces as senhub.unifi.up=0,
// not a collection error.
func (p *unifiProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	baseTags := []tags.Tag{
		{Key: "endpoint", Value: p.cfg.Endpoint},
		{Key: "site", Value: p.cfg.Site},
		{Key: "metric_type", Value: "controller"},
	}

	var points []data_store.DataPoint
	emitUp := func(up float32) []data_store.DataPoint {
		points = append([]data_store.DataPoint{
			{Name: "senhub.unifi.up", Value: up, Timestamp: now, Tags: baseTags},
		}, points...)
		return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName())
	}

	if err := p.login(); err != nil {
		p.moduleLogger.Warn().Err(err).Str("endpoint", p.cfg.Endpoint).Msg("unifi login failed")
		p.entitySource.markReachable(false)
		return emitUp(0), nil
	}

	health, err := p.fetchHealth()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("unifi health fetch failed")
		p.entitySource.markReachable(false)
		return emitUp(0), nil
	}
	devices, err := p.fetchDevices()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("unifi device fetch failed")
		p.entitySource.markReachable(false)
		return emitUp(0), nil
	}
	clients, err := p.fetchClients()
	if err != nil {
		p.moduleLogger.Warn().Err(err).Msg("unifi client fetch failed")
		p.entitySource.markReachable(false)
		return emitUp(0), nil
	}

	p.entitySource.markReachable(true)
	points = append(points, p.buildNetworkPoints(health, baseTags, now)...)
	points = append(points, p.buildDevicePoints(devices, now)...)
	points = append(points, p.buildClientPoints(clients, baseTags, now)...)

	return emitUp(1), nil
}

// login posts credentials and lets the cookie jar capture the session.
func (p *unifiProbe) login() error {
	body, err := json.Marshal(map[string]string{
		"username": p.cfg.Username,
		"password": p.cfg.Password,
	})
	if err != nil {
		return fmt.Errorf("encoding login body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, p.cfg.Endpoint+"/api/login", bytes.NewReader(body))
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
		return fmt.Errorf("login returned status %d", resp.StatusCode)
	}
	return nil
}

// getJSON issues an authenticated GET and decodes the controller's
// {"data": [...]} envelope into out.
func (p *unifiProbe) getJSON(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, p.cfg.Endpoint+path, nil)
	if err != nil {
		return fmt.Errorf("building request %s: %w", path, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", path, resp.StatusCode)
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

func (p *unifiProbe) fetchHealth() (healthEnvelope, error) {
	var env healthEnvelope
	err := p.getJSON(p.sitePath("/stat/health"), &env)
	return env, err
}

// deviceEnvelope is the subset of stat/device consumed for inventory and
// per-device health.
type deviceEnvelope struct {
	Data []deviceRow `json:"data"`
}

type deviceRow struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	State    int     `json:"state"`
	Adopted  bool    `json:"adopted"`
	NumSta   float64 `json:"num_sta"`
	Score    float64 `json:"satisfaction"`
	SysStats struct {
		CPU string `json:"cpu"`
		Mem string `json:"mem"`
	} `json:"system-stats"`
}

func (p *unifiProbe) fetchDevices() (deviceEnvelope, error) {
	var env deviceEnvelope
	err := p.getJSON(p.sitePath("/stat/device"), &env)
	return env, err
}

// clientEnvelope is the subset of stat/sta consumed for client counts.
type clientEnvelope struct {
	Data []struct {
		IsWired bool `json:"is_wired"`
	} `json:"data"`
}

func (p *unifiProbe) fetchClients() (clientEnvelope, error) {
	var env clientEnvelope
	err := p.getJSON(p.sitePath("/stat/sta"), &env)
	return env, err
}

// buildNetworkPoints emits the WAN throughput counters from the wan
// subsystem of stat/health.
func (p *unifiProbe) buildNetworkPoints(health healthEnvelope, baseTags []tags.Tag, ts time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint
	for _, h := range health.Data {
		if h.Subsystem != "wan" {
			continue
		}
		points = append(points,
			data_store.DataPoint{Name: "unifi.network.tx_bytes", Value: float32(h.TxBytesR), Timestamp: ts, Tags: baseTags},
			data_store.DataPoint{Name: "unifi.network.rx_bytes", Value: float32(h.RxBytesR), Timestamp: ts, Tags: baseTags},
		)
	}
	return points
}

// buildDevicePoints emits the per-type inventory rollups and the
// per-device cpu/memory/satisfaction gauges.
func (p *unifiProbe) buildDevicePoints(devices deviceEnvelope, ts time.Time) []data_store.DataPoint {
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

		devTags := []tags.Tag{
			{Key: "endpoint", Value: p.cfg.Endpoint},
			{Key: "site", Value: p.cfg.Site},
			{Key: "device_name", Value: deviceName(d)},
			{Key: "device_type", Value: dt},
			{Key: "metric_type", Value: "device"},
		}
		if cpu, ok := parseFloat(d.SysStats.CPU); ok {
			points = append(points, data_store.DataPoint{Name: "unifi.device.cpu", Value: float32(cpu), Timestamp: ts, Tags: devTags})
		}
		if mem, ok := parseFloat(d.SysStats.Mem); ok {
			points = append(points, data_store.DataPoint{Name: "unifi.device.memory", Value: float32(mem), Timestamp: ts, Tags: devTags})
		}
		if dt == "uap" {
			apTags := []tags.Tag{
				{Key: "endpoint", Value: p.cfg.Endpoint},
				{Key: "site", Value: p.cfg.Site},
				{Key: "device_name", Value: deviceName(d)},
				{Key: "metric_type", Value: "access_point"},
			}
			points = append(points,
				data_store.DataPoint{Name: "unifi.ap.clients", Value: float32(d.NumSta), Timestamp: ts, Tags: apTags},
				data_store.DataPoint{Name: "unifi.ap.satisfaction", Value: float32(d.Score / 100), Timestamp: ts, Tags: apTags},
			)
		}
	}

	for dt, c := range byType {
		typeTags := []tags.Tag{
			{Key: "endpoint", Value: p.cfg.Endpoint},
			{Key: "site", Value: p.cfg.Site},
			{Key: "device_type", Value: dt},
			{Key: "metric_type", Value: "inventory"},
		}
		points = append(points,
			data_store.DataPoint{Name: "unifi.devices.total", Value: float32(c.total), Timestamp: ts, Tags: typeTags},
			data_store.DataPoint{Name: "unifi.devices.adopted", Value: float32(c.adopted), Timestamp: ts, Tags: typeTags},
			data_store.DataPoint{Name: "unifi.devices.disconnected", Value: float32(c.disconnected), Timestamp: ts, Tags: typeTags},
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
		{Name: "unifi.clients.total", Value: float32(total), Timestamp: ts, Tags: baseTags},
		{Name: "unifi.clients.wifi", Value: float32(wifi), Timestamp: ts, Tags: baseTags},
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
