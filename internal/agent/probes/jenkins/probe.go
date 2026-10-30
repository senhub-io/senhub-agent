package jenkins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier — part of licence JWT
// claims, the transformer file path and the DiscriminantTagsRegistry key.
const ProbeType = "jenkins"

const (
	defaultInterval = 60 * time.Second
	defaultTimeout  = 15 * time.Second

	// jobsTree limits the job listing to the fields the probe consumes so
	// the controller does not serialise the full (potentially huge) job
	// graph each cycle.
	jobsTree = "jobs[name,color,lastBuild[number,duration,result,timestamp]]"

	metricUp              = "senhub.jenkins.up"
	metricJobCount        = "senhub.jenkins.job.count"
	metricJobDuration     = "senhub.jenkins.job.duration"
	metricJobLastBuildNum = "senhub.jenkins.job.last_build_number"
	metricNodeCount       = "senhub.jenkins.node.count"
	metricNodeExecutor    = "senhub.jenkins.node.executor.count"
	metricQueueSize       = "senhub.jenkins.queue.size"
	metricQueueBlocked    = "senhub.jenkins.queue.blocked"
)

type jenkinsConfig struct {
	Endpoint     string
	Username     string
	APIToken     string
	Interval     time.Duration
	Timeout      time.Duration
	InstanceName string // optional operator-supplied stable id (precedence 1)
}

type httpStatusError struct {
	code int
	msg  string
}

func (e *httpStatusError) Error() string { return e.msg }

// jenkinsProbe monitors a single Jenkins controller.
type jenkinsProbe struct {
	*types.BaseProbe
	cfg          jenkinsConfig
	instance     string // host:port — the stable target id
	moduleLogger *logger.ModuleLogger
	client       *http.Client
	entitySource *entitySource
}

// NewJenkinsProbe builds a jenkins probe from its raw params block.
func NewJenkinsProbe(rawConfig map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	cfg, err := parseConfig(rawConfig)
	if err != nil {
		return nil, err
	}

	host, port, err := hostPort(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	instance := host + ":" + port

	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.jenkins")
	moduleLogger.Debug().
		Str("endpoint", cfg.Endpoint).
		Str("instance", instance).
		Bool("authenticated", cfg.Username != "").
		Msg("Creating new Jenkins probe")

	httpClient := &http.Client{Timeout: cfg.Timeout}
	probe := &jenkinsProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		instance:     instance,
		moduleLogger: moduleLogger,
		client:       httpClient,
	}
	probe.SetProbeType(ProbeType)

	// Build the entity source. fetchIdentity is a closure over the probe's
	// HTTP client so it reuses existing auth and timeout config.
	// hostIDFn uses the OS machine-id as the precedence-2 fallback.
	hostIDFn := func() string {
		id, err := common.GetHostIdentity()
		if err != nil {
			return ""
		}
		return id.ID
	}
	probe.entitySource = newEntitySource(
		cfg.InstanceName,
		host,
		port,
		func() (string, error) {
			return fetchInstanceIdentity(func(path string, out interface{}) error {
				return probe.getJSON(context.Background(), path, out)
			})
		},
		hostIDFn,
	)
	probe.SetEntitySource(probe.entitySource)
	return probe, nil
}

func parseConfig(config map[string]interface{}) (jenkinsConfig, error) {
	cfg := jenkinsConfig{
		Interval: defaultInterval,
		Timeout:  defaultTimeout,
	}

	endpoint, _ := config["endpoint"].(string)
	if endpoint == "" {
		return cfg, fmt.Errorf("jenkins requires an endpoint (e.g. https://jenkins.example.com)")
	}
	cfg.Endpoint = endpoint

	if v, ok := config["username"].(string); ok {
		cfg.Username = v
	}
	if v, ok := config["api_token"].(string); ok {
		cfg.APIToken = v
	}
	if v, ok := config["instance_name"].(string); ok {
		cfg.InstanceName = v
	}
	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, ok := types.IntParam(config, "timeout"); ok && v > 0 {
		cfg.Timeout = time.Duration(v) * time.Second
	}
	return cfg, nil
}

// hostPort extracts host:port from the endpoint, defaulting the port to
// the scheme's standard (8080 is common for Jenkins but the scheme port is
// the safe neutral default when none is given).
func hostPort(endpoint string) (string, string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("jenkins endpoint %q is not a valid URL: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", fmt.Errorf("jenkins endpoint %q must use http or https, got scheme %q", endpoint, u.Scheme)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port, nil
}

func (p *jenkinsProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *jenkinsProbe) ShouldStart() bool          { return true }
func (p *jenkinsProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *jenkinsProbe) OnStart(_ chan struct{}) error {
	p.moduleLogger.Info().
		Str("instance", p.instance).
		Msg("Starting jenkins probe")
	return nil
}

func (p *jenkinsProbe) OnShutdown(_ context.Context) error {
	p.client.CloseIdleConnections()
	return nil
}

// Collect collects without a cancellation signal; the poller calls
// CollectContext.
func (p *jenkinsProbe) Collect() ([]data_store.DataPoint, error) {
	return p.CollectContext(context.Background())
}

// CollectContext runs one cycle. A failing controller is a measurement
// (senhub.jenkins.up=0), never a collection error — mirroring the
// always-emit-up contract of the other active-check probes.
func (p *jenkinsProbe) CollectContext(ctx context.Context) ([]data_store.DataPoint, error) {
	now := time.Now()
	var points []data_store.DataPoint

	jobs, jobsErr := p.fetchJobs(ctx)
	nodes, nodesErr := p.fetchNodes(ctx)
	queue, queueErr := p.fetchQueue(ctx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("jenkins collection interrupted: %w", ctxErr)
	}

	identityTags := p.baseTags()

	up := float64(1)
	if jobsErr != nil {
		up = 0
		p.moduleLogger.Warn().Err(jobsErr).Str("instance", p.instance).Msg("jenkins jobs query failed")
	}
	points = append(points, data_store.DataPoint{
		Name:      metricUp,
		Value:     up,
		Timestamp: now,
		Tags: combineTags([]tags.Tag{
			{Key: "instance", Value: p.instance},
			{Key: "metric_type", Value: "status"},
		}, identityTags...),
	})

	if jobsErr == nil {
		points = append(points, p.buildJobPoints(jobs, now, identityTags)...)
	}
	if nodesErr == nil {
		points = append(points, p.buildNodePoints(nodes, now, identityTags)...)
	} else {
		p.moduleLogger.Warn().Err(nodesErr).Str("instance", p.instance).Msg("jenkins nodes query failed")
	}
	if queueErr == nil {
		points = append(points, p.buildQueuePoints(queue, now, identityTags)...)
	} else {
		p.moduleLogger.Warn().Err(queueErr).Str("instance", p.instance).Msg("jenkins queue query failed")
	}

	checkErr := jobsErr
	if checkErr == nil {
		checkErr = nodesErr
	}
	if checkErr == nil {
		checkErr = queueErr
	}
	points = append(points, data_store.DataPoint{
		Name:      dbcommon.CheckResultMetric,
		Value:     float64(classifyJenkinsError(checkErr)),
		Timestamp: now,
		Tags: combineTags([]tags.Tag{
			{Key: "metric_type", Value: "status"},
		}, identityTags...),
	})

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// baseTags returns the entity identity tags shared by every datapoint this
// probe emits. The id is resolved by the same entity source that emits the
// service.instance entity, so the two values cannot diverge.
func (p *jenkinsProbe) baseTags() []tags.Tag {
	if id := p.entitySource.serviceInstanceID(); id != "" {
		return []tags.Tag{{Key: idKeyServiceInstanceID, Value: id}}
	}
	return nil
}

func classifyJenkinsError(err error) dbcommon.CheckResult {
	if err == nil {
		return dbcommon.CheckOK
	}
	var httpErr *httpStatusError
	if errors.As(err, &httpErr) {
		return dbcommon.ClassifyHTTPStatus(httpErr.code)
	}
	return dbcommon.ClassifyError(err)
}

func combineTags(first []tags.Tag, extra ...tags.Tag) []tags.Tag {
	out := make([]tags.Tag, 0, len(first)+len(extra))
	out = append(out, first...)
	out = append(out, extra...)
	return out
}

// --- wire shapes -----------------------------------------------------------

type jobsResponse struct {
	Jobs []job `json:"jobs"`
}

type job struct {
	Name      string     `json:"name"`
	Color     string     `json:"color"`
	LastBuild *lastBuild `json:"lastBuild"`
}

type lastBuild struct {
	Number    int64  `json:"number"`
	Duration  int64  `json:"duration"` // milliseconds
	Result    string `json:"result"`
	Timestamp int64  `json:"timestamp"`
}

type computerResponse struct {
	Computers []computer `json:"computer"`
}

type computer struct {
	DisplayName  string     `json:"displayName"`
	Offline      bool       `json:"offline"`
	Executors    []executor `json:"executors"`
	NumExecutors int        `json:"numExecutors"`
}

type executor struct {
	Idle bool `json:"idle"`
}

type queueResponse struct {
	Items []queueItem `json:"items"`
}

type queueItem struct {
	Blocked bool `json:"blocked"`
}

// --- metric builders -------------------------------------------------------

// buildJobPoints maps the job tree to:
//   - job.count collapsed by build status (success/failure/unstable/aborted);
//   - per-job duration (ms) and last build number, split by job name.
//
// Job status is read from lastBuild.result, falling back to the colour ball
// when no build has run yet. Jobs with no build are not counted (no status).
func (p *jenkinsProbe) buildJobPoints(jobs []job, ts time.Time, baseTags []tags.Tag) []data_store.DataPoint {
	statusCounts := map[string]int{
		"success":  0,
		"failure":  0,
		"unstable": 0,
		"aborted":  0,
	}
	var points []data_store.DataPoint

	for _, j := range jobs {
		if j.LastBuild == nil {
			continue
		}
		status := normalizeJobStatus(j.LastBuild.Result, j.Color)
		if status == "" {
			continue
		}
		if _, known := statusCounts[status]; known {
			statusCounts[status]++
		}

		jobTags := combineTags(baseTags,
			tags.Tag{Key: "job", Value: j.Name},
			tags.Tag{Key: "metric_type", Value: "jobs"},
		)
		points = append(points,
			data_store.DataPoint{Name: metricJobDuration, Value: float64(j.LastBuild.Duration), Timestamp: ts, Tags: jobTags},
			data_store.DataPoint{Name: metricJobLastBuildNum, Value: float64(j.LastBuild.Number), Timestamp: ts, Tags: jobTags},
		)
	}

	for status, count := range statusCounts {
		points = append(points, data_store.DataPoint{
			Name: metricJobCount, Value: float64(count), Timestamp: ts,
			Tags: combineTags(baseTags,
				tags.Tag{Key: "status", Value: status},
				tags.Tag{Key: "metric_type", Value: "jobs"},
			),
		})
	}
	return points
}

// buildNodePoints maps the computer view to:
//   - node.count collapsed by online/offline;
//   - executor.count collapsed by busy/free across all online nodes.
func (p *jenkinsProbe) buildNodePoints(nodes []computer, ts time.Time, baseTags []tags.Tag) []data_store.DataPoint {
	online, offline := 0, 0
	busy, free := 0, 0

	for _, n := range nodes {
		if n.Offline {
			offline++
			continue
		}
		online++
		for _, e := range n.Executors {
			if e.Idle {
				free++
			} else {
				busy++
			}
		}
	}

	mt := combineTags(baseTags, tags.Tag{Key: "metric_type", Value: "nodes"})
	statusTag := func(status string) []tags.Tag {
		return combineTags([]tags.Tag{{Key: "status", Value: status}}, mt...)
	}
	stateTag := func(state string) []tags.Tag {
		return combineTags([]tags.Tag{{Key: "state", Value: state}}, mt...)
	}

	return []data_store.DataPoint{
		{Name: metricNodeCount, Value: float64(online), Timestamp: ts, Tags: statusTag("online")},
		{Name: metricNodeCount, Value: float64(offline), Timestamp: ts, Tags: statusTag("offline")},
		{Name: metricNodeExecutor, Value: float64(busy), Timestamp: ts, Tags: stateTag("busy")},
		{Name: metricNodeExecutor, Value: float64(free), Timestamp: ts, Tags: stateTag("free")},
	}
}

// buildQueuePoints maps the build queue to its size and its blocked count.
func (p *jenkinsProbe) buildQueuePoints(q queueResponse, ts time.Time, baseTags []tags.Tag) []data_store.DataPoint {
	blocked := 0
	for _, item := range q.Items {
		if item.Blocked {
			blocked++
		}
	}
	mt := combineTags(baseTags, tags.Tag{Key: "metric_type", Value: "queue"})
	return []data_store.DataPoint{
		{Name: metricQueueSize, Value: float64(len(q.Items)), Timestamp: ts, Tags: mt},
		{Name: metricQueueBlocked, Value: float64(blocked), Timestamp: ts, Tags: mt},
	}
}

// normalizeJobStatus maps a lastBuild.result (SUCCESS/FAILURE/UNSTABLE/
// ABORTED) to its lowercase status; when result is empty (a build in
// progress reports no result yet) it falls back to the colour ball.
func normalizeJobStatus(result, color string) string {
	switch result {
	case "SUCCESS":
		return "success"
	case "FAILURE":
		return "failure"
	case "UNSTABLE":
		return "unstable"
	case "ABORTED":
		return "aborted"
	case "":
		// In-progress build: the colour ball carries the previous outcome
		// with an "_anime" suffix (blue_anime, red_anime, …).
		switch color {
		case "blue", "blue_anime", "green", "green_anime":
			return "success"
		case "red", "red_anime":
			return "failure"
		case "yellow", "yellow_anime":
			return "unstable"
		case "aborted", "aborted_anime":
			return "aborted"
		}
	}
	return ""
}

// --- HTTP plumbing ---------------------------------------------------------

func (p *jenkinsProbe) fetchJobs(ctx context.Context) ([]job, error) {
	var resp jobsResponse
	if err := p.getJSON(ctx, "/api/json?tree="+url.QueryEscape(jobsTree), &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

func (p *jenkinsProbe) fetchNodes(ctx context.Context) ([]computer, error) {
	var resp computerResponse
	if err := p.getJSON(ctx, "/computer/api/json", &resp); err != nil {
		return nil, err
	}
	return resp.Computers, nil
}

func (p *jenkinsProbe) fetchQueue(ctx context.Context) (queueResponse, error) {
	var resp queueResponse
	if err := p.getJSON(ctx, "/queue/api/json", &resp); err != nil {
		return queueResponse{}, err
	}
	return resp, nil
}

// getJSON issues an authenticated GET against the controller and decodes the
// JSON body into out. BasicAuth (username:api_token) is sent only when a
// username is configured, so an anonymous-read controller works too.
func (p *jenkinsProbe) getJSON(ctx context.Context, path string, out interface{}) error {
	full := p.cfg.Endpoint + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", path, err)
	}
	if p.cfg.Username != "" {
		req.SetBasicAuth(p.cfg.Username, p.cfg.APIToken)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("requesting %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &httpStatusError{
			code: resp.StatusCode,
			msg:  fmt.Sprintf("jenkins %s returned HTTP %d: %s", path, resp.StatusCode, string(body)),
		}
	}

	// service.version rides the entity from the X-Jenkins header (present on
	// every Jenkins response).
	if p.entitySource != nil {
		p.entitySource.setVersion(resp.Header.Get("X-Jenkins"))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding %s response: %w", path, err)
	}
	return nil
}
