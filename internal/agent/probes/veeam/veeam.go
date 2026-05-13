// Package veeam provides monitoring capabilities for Veeam Backup & Replication v13 via REST API
package veeam

import (
	"context"
	"fmt"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// veeamProbe implements monitoring for Veeam Backup & Replication using the REST API
type veeamProbe struct {
	*types.BaseProbe
	config       map[string]interface{}
	logger       *logger.ModuleLogger
	interval     time.Duration
	client       *veeamClient
	endpoint     string
	username     string
	password     string
	verifySSL    bool
	hoursToCheck int
	ctx          context.Context
	cancelFunc   context.CancelFunc
}

// NewVeeamProbe creates a new instance of the Veeam probe
func NewVeeamProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.veeam")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	interval := time.Duration(cfg.Interval) * time.Second

	ctx, cancel := context.WithCancel(context.Background())

	probe := &veeamProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       config,
		logger:       moduleLogger,
		interval:     interval,
		endpoint:     cfg.Endpoint,
		username:     cfg.Username,
		password:     cfg.Password,
		verifySSL:    cfg.VerifySSL,
		hoursToCheck: cfg.HoursToCheck,
		ctx:          ctx,
		cancelFunc:   cancel,
	}

	return probe, nil
}

// parseConfig extracts and validates configuration parameters
func parseConfig(config map[string]interface{}) (*probeConfig, error) {
	endpoint, ok := config["endpoint"].(string)
	if !ok || endpoint == "" {
		return nil, fmt.Errorf("veeam probe requires 'endpoint' configuration")
	}

	username, ok := config["username"].(string)
	if !ok || username == "" {
		return nil, fmt.Errorf("veeam probe requires 'username' configuration")
	}

	password, ok := config["password"].(string)
	if !ok || password == "" {
		return nil, fmt.Errorf("veeam probe requires 'password' configuration")
	}

	interval := 300
	if cfgInterval, ok := config["interval"].(int); ok {
		interval = cfgInterval
	}

	verifySSL := true
	if cfgVerifySSL, ok := config["verify_ssl"].(bool); ok {
		verifySSL = cfgVerifySSL
	}

	hoursToCheck := 24
	if cfgHours, ok := config["hours_to_check"].(int); ok {
		hoursToCheck = cfgHours
	}

	port := 9419
	if cfgPort, ok := config["port"].(int); ok {
		port = cfgPort
	}

	// Build full endpoint URL: append port if not already present in the URL
	fullEndpoint := strings.TrimSuffix(endpoint, "/")
	hostPart := strings.TrimPrefix(strings.TrimPrefix(fullEndpoint, "https://"), "http://")
	if !strings.Contains(hostPart, ":") {
		fullEndpoint = fmt.Sprintf("%s:%d", fullEndpoint, port)
	}

	return &probeConfig{
		Endpoint:     fullEndpoint,
		Username:     username,
		Password:     password,
		Interval:     interval,
		Port:         port,
		VerifySSL:    verifySSL,
		HoursToCheck: hoursToCheck,
	}, nil
}

// ShouldStart indicates if probe should be activated
func (p *veeamProbe) ShouldStart() bool {
	return true
}

// GetInterval returns the collection frequency
func (p *veeamProbe) GetInterval() time.Duration {
	return p.interval
}

// OnStart initializes the probe when it's started
func (p *veeamProbe) OnStart(quitChannel chan struct{}) error {
	p.client = newVeeamClient(p.endpoint, p.username, p.password, p.verifySSL, p.ctx, p.logger.Logger)

	// Validate connectivity by fetching server info
	info, err := p.client.GetServerInfo()
	if err != nil {
		return fmt.Errorf("failed to connect to Veeam API at %s: %w", p.endpoint, err)
	}

	p.logger.Info().
		Str("endpoint", p.endpoint).
		Str("server_name", info.Name).
		Str("version", info.BuildVersion).
		Str("platform", info.Platform).
		Bool("verify_ssl", p.verifySSL).
		Int("hours_to_check", p.hoursToCheck).
		Msg("Veeam probe initialized")

	return nil
}

// Collect gathers metrics and returns collected datapoints
func (p *veeamProbe) Collect() ([]datapoint.DataPoint, error) {
	if p.client == nil {
		return nil, fmt.Errorf("veeam client not initialized")
	}

	now := time.Now()
	var allDatapoints []datapoint.DataPoint

	commonTags := []tags.Tag{
		{Key: "endpoint", Value: p.endpoint},
	}

	// Collect job states (single API call replaces N+1 pattern)
	jobPoints, err := p.collectJobMetrics(now)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect job metrics, continuing with other endpoints")
	} else {
		allDatapoints = append(allDatapoints, jobPoints...)
	}

	// Collect repository metrics
	repoPoints, err := p.collectRepositoryMetrics(now)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect repository metrics, continuing with other endpoints")
	} else {
		allDatapoints = append(allDatapoints, repoPoints...)
	}

	// Collect license metrics
	licPoints, err := p.collectLicenseMetrics(now)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect license metrics, continuing with other endpoints")
	} else {
		allDatapoints = append(allDatapoints, licPoints...)
	}

	// Collect proxy metrics
	proxyPoints, err := p.collectProxyMetrics(now)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect proxy metrics, continuing with other endpoints")
	} else {
		allDatapoints = append(allDatapoints, proxyPoints...)
	}

	// Collect backup objects (restore points, protection status)
	objPoints, err := p.collectBackupObjectMetrics(now)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect backup object metrics, continuing with other endpoints")
	} else {
		allDatapoints = append(allDatapoints, objPoints...)
	}

	// Collect managed servers (infrastructure health)
	srvPoints, err := p.collectManagedServerMetrics(now)
	if err != nil {
		p.logger.Warn().Err(err).Msg("Failed to collect managed server metrics, continuing with other endpoints")
	} else {
		allDatapoints = append(allDatapoints, srvPoints...)
	}

	// Add common tags to all datapoints
	for i := range allDatapoints {
		allDatapoints[i].Tags = append(allDatapoints[i].Tags, commonTags...)
	}

	// Enrich with probe name
	enrichedDatapoints := p.BaseProbe.EnrichDataPointsWithProbeName(allDatapoints, p.GetName())

	p.logger.Debug().
		Int("datapoints_count", len(enrichedDatapoints)).
		Msg("Veeam metrics collection completed")

	return enrichedDatapoints, nil
}

// collectJobMetrics uses /jobs/states for a single-call consolidated view
func (p *veeamProbe) collectJobMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	states, err := p.client.GetJobStates()
	if err != nil {
		return nil, fmt.Errorf("failed to get job states: %w", err)
	}

	// Surface any bottleneck strings the Veeam API returned outside our
	// known mapping. The metric itself falls back to 0 (= None) so the
	// PRTG channel stays populated; this WARN exists so an operator can
	// see API drift in the log and add the new value to bottleneckMapping.
	for _, s := range states {
		if s.SessionProgress == nil {
			continue
		}
		if _, ok := bottleneckMapping[s.SessionProgress.Bottleneck]; !ok && s.SessionProgress.Bottleneck != "" {
			p.logger.Warn().
				Str("job_name", s.Name).
				Str("bottleneck_raw", s.SessionProgress.Bottleneck).
				Msg("unknown Veeam bottleneck value — emitting as None (0); add to bottleneckMapping if this is a legitimate API value")
		}
	}

	var points []datapoint.DataPoint
	points = append(points, buildJobStateOverviewMetrics(states, p.hoursToCheck, now)...)
	points = append(points, buildJobStateDetailMetrics(states, p.hoursToCheck, now)...)

	return points, nil
}

// collectRepositoryMetrics fetches repositories and builds capacity metrics
func (p *veeamProbe) collectRepositoryMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	repos, err := p.client.GetRepositories()
	if err != nil {
		return nil, fmt.Errorf("failed to get repositories: %w", err)
	}

	return buildRepositoryMetrics(repos, now), nil
}

// collectLicenseMetrics fetches license info and builds metrics
func (p *veeamProbe) collectLicenseMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	lic, err := p.client.GetLicense()
	if err != nil {
		return nil, fmt.Errorf("failed to get license info: %w", err)
	}

	return buildLicenseMetrics(lic, now), nil
}

// collectProxyMetrics fetches proxies and builds status metrics
func (p *veeamProbe) collectProxyMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	proxies, err := p.client.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("failed to get proxies: %w", err)
	}

	return buildProxyMetrics(proxies, now), nil
}

// collectBackupObjectMetrics fetches protected objects and builds metrics
func (p *veeamProbe) collectBackupObjectMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	objects, err := p.client.GetBackupObjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get backup objects: %w", err)
	}

	return buildBackupObjectMetrics(objects, now), nil
}

// collectManagedServerMetrics fetches infrastructure servers and builds status metrics
func (p *veeamProbe) collectManagedServerMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	servers, err := p.client.GetManagedServers()
	if err != nil {
		return nil, fmt.Errorf("failed to get managed servers: %w", err)
	}

	return buildManagedServerMetrics(servers, now), nil
}

// OnShutdown handles cleanup when probe is stopped
func (p *veeamProbe) OnShutdown(ctx context.Context) error {
	p.logger.Debug().Msg("Shutting down Veeam probe")
	p.cancelFunc()
	return nil
}

// GetTargetStrategies returns the strategies this probe's data should be sent to
func (p *veeamProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}
