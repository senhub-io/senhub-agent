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

	// Collect jobs and sessions
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

	// Add common tags to all datapoints
	for i := range allDatapoints {
		allDatapoints[i].Tags = append(allDatapoints[i].Tags, commonTags...)
	}

	// Enrich with probe name
	enrichedDatapoints := p.BaseProbe.EnrichDataPointsWithProbeName(allDatapoints, p.GetName())

	// Route data through callback if configured
	if p.OnDataPoints != nil && len(enrichedDatapoints) > 0 {
		if err := p.OnDataPoints(enrichedDatapoints, p); err != nil {
			return nil, fmt.Errorf("error handling data points: %w", err)
		}
	}

	p.logger.Debug().
		Int("datapoints_count", len(enrichedDatapoints)).
		Msg("Veeam metrics collection completed")

	return enrichedDatapoints, nil
}

// collectJobMetrics fetches jobs and their latest sessions, then builds metrics
func (p *veeamProbe) collectJobMetrics(now time.Time) ([]datapoint.DataPoint, error) {
	jobs, err := p.client.GetJobs()
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs: %w", err)
	}

	sessionsByJob := make(map[string][]session)
	for _, j := range jobs {
		if j.IsDisabled {
			continue
		}
		sessions, err := p.client.GetSessions(j.ID, 1)
		if err != nil {
			p.logger.Warn().
				Err(err).
				Str("job_name", j.Name).
				Msg("Failed to get sessions for job, skipping")
			continue
		}
		sessionsByJob[j.ID] = sessions
	}

	var points []datapoint.DataPoint
	points = append(points, buildJobOverviewMetrics(jobs, sessionsByJob, p.hoursToCheck, now)...)
	points = append(points, buildJobDetailMetrics(jobs, sessionsByJob, p.hoursToCheck, now)...)

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

// OnShutdown handles cleanup when probe is stopped
func (p *veeamProbe) OnShutdown(ctx context.Context) error {
	p.logger.Debug().Msg("Shutting down Veeam probe")
	p.cancelFunc()
	return nil
}

// GetTargetStrategies returns the strategies this probe's data should be sent to
func (p *veeamProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http"}
}
