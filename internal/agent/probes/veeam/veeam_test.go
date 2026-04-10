package veeam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// newTestLogger creates a zerolog logger suitable for tests
func newTestLogger() *logger.Logger {
	l := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	return (*logger.Logger)(&l)
}

// --- TestNewVeeamProbe ---

func TestNewVeeamProbe_ValidConfig(t *testing.T) {
	config := map[string]interface{}{
		"endpoint": "https://veeam-srv:9419",
		"username": "admin",
		"password": "secret",
	}
	probe, err := NewVeeamProbe(config, newTestLogger())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if probe == nil {
		t.Fatal("expected probe, got nil")
	}
}

func TestNewVeeamProbe_MissingEndpoint(t *testing.T) {
	config := map[string]interface{}{
		"username": "admin",
		"password": "secret",
	}
	_, err := NewVeeamProbe(config, newTestLogger())
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestNewVeeamProbe_MissingUsername(t *testing.T) {
	config := map[string]interface{}{
		"endpoint": "https://veeam-srv:9419",
		"password": "secret",
	}
	_, err := NewVeeamProbe(config, newTestLogger())
	if err == nil {
		t.Fatal("expected error for missing username")
	}
}

func TestNewVeeamProbe_MissingPassword(t *testing.T) {
	config := map[string]interface{}{
		"endpoint": "https://veeam-srv:9419",
		"username": "admin",
	}
	_, err := NewVeeamProbe(config, newTestLogger())
	if err == nil {
		t.Fatal("expected error for missing password")
	}
}

// --- TestParseConfig ---

func TestParseConfig_Defaults(t *testing.T) {
	config := map[string]interface{}{
		"endpoint": "https://veeam-srv:9419",
		"username": "admin",
		"password": "secret",
	}
	cfg, err := parseConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Interval != 300 {
		t.Errorf("expected default interval 300, got %d", cfg.Interval)
	}
	if cfg.HoursToCheck != 24 {
		t.Errorf("expected default hours_to_check 24, got %d", cfg.HoursToCheck)
	}
	if !cfg.VerifySSL {
		t.Error("expected default verify_ssl true")
	}
}

func TestParseConfig_CustomValues(t *testing.T) {
	config := map[string]interface{}{
		"endpoint":       "https://veeam-srv:9419",
		"username":       "admin",
		"password":       "secret",
		"interval":       120,
		"verify_ssl":     false,
		"hours_to_check": 48,
	}
	cfg, err := parseConfig(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Interval != 120 {
		t.Errorf("expected interval 120, got %d", cfg.Interval)
	}
	if cfg.HoursToCheck != 48 {
		t.Errorf("expected hours_to_check 48, got %d", cfg.HoursToCheck)
	}
	if cfg.VerifySSL {
		t.Error("expected verify_ssl false")
	}
}

// --- TestJobStatusMapping ---

func TestJobStatusMapping(t *testing.T) {
	tests := []struct {
		result   string
		expected float32
	}{
		{"None", 0},
		{"Success", 1},
		{"Warning", 2},
		{"Failed", 3},
		{"Unknown", 0},
	}
	for _, tc := range tests {
		got := jobStatusValue(tc.result)
		if got != tc.expected {
			t.Errorf("jobStatusValue(%q) = %v, want %v", tc.result, got, tc.expected)
		}
	}
}

func TestJobStateValue_Running(t *testing.T) {
	if got := jobStateValue("Working"); got != 4 {
		t.Errorf("jobStateValue(Working) = %v, want 4", got)
	}
	if got := jobStateValue("Stopped"); got != 0 {
		t.Errorf("jobStateValue(Stopped) = %v, want 0", got)
	}
}

// --- TestTokenRefresh ---

func TestTokenRefresh(t *testing.T) {
	callCount := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth2/token" {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: fmt.Sprintf("token-%d", callCount),
				TokenType:   "bearer",
				ExpiresIn:   900,
			})
			return
		}
		// For any other request, return a simple JSON response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"test","buildVersion":"13.0","platform":"Windows"}`))
	}))
	defer ts.Close()

	client := &veeamClient{
		httpClient: ts.Client(),
		endpoint:   ts.URL,
		username:   "admin",
		password:   "secret",
		ctx:        context.Background(),
		logger:     logger.NewModuleLogger(newTestLogger(), "probe.veeam.client"),
	}

	// First call should authenticate
	if err := client.authenticate(); err != nil {
		t.Fatalf("first authenticate failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 auth call, got %d", callCount)
	}

	// Second call should use cached token
	if err := client.authenticate(); err != nil {
		t.Fatalf("second authenticate failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 auth call (cached), got %d", callCount)
	}

	// Force expiry - set to within 60s margin
	client.tokenMu.Lock()
	client.tokenExpiry = time.Now().Add(30 * time.Second)
	client.tokenMu.Unlock()

	// Third call should re-authenticate
	if err := client.authenticate(); err != nil {
		t.Fatalf("third authenticate failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 auth calls after expiry, got %d", callCount)
	}
}

func TestIsTokenValid(t *testing.T) {
	client := &veeamClient{
		logger: logger.NewModuleLogger(newTestLogger(), "probe.veeam.client"),
	}

	// No token
	if client.isTokenValid() {
		t.Error("expected invalid when no token set")
	}

	// Valid token
	client.token = "valid"
	client.tokenExpiry = time.Now().Add(5 * time.Minute)
	if !client.isTokenValid() {
		t.Error("expected valid token")
	}

	// Token about to expire (within 60s margin)
	client.tokenExpiry = time.Now().Add(30 * time.Second)
	if client.isTokenValid() {
		t.Error("expected invalid when token expires within 60s margin")
	}
}

// --- TestCollectMetrics ---

func TestCollectMetrics(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/oauth2/token":
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "test-token",
				TokenType:   "bearer",
				ExpiresIn:   900,
			})
		case r.URL.Path == "/api/v1/serverInfo":
			_, _ = w.Write([]byte(`{"platform":"Windows","name":"SIEP-BCK","buildVersion":"13.0.1.180"}`))
		case r.URL.Path == "/api/v1/jobs":
			_, _ = w.Write([]byte(`{"data":[
				{"type":"HyperVBackup","id":"7d5a054a","name":"BCK_HyperV","isDisabled":false},
				{"type":"WindowsAgentBackup","id":"bc48c775","name":"BCK_SIEP-FLORIAN","isDisabled":false}
			],"pagination":{"total":2,"count":2,"skip":0,"limit":200}}`))
		case r.URL.Path == "/api/v1/sessions":
			jobID := r.URL.Query().Get("jobId")
			if jobID == "7d5a054a" {
				_, _ = w.Write([]byte(`{"data":[
					{"id":"sess1","name":"BCK_HyperV","result":"Success","state":"Stopped","creationTime":"2026-04-09T02:00:00Z","endTime":"2026-04-09T03:30:00Z"}
				],"pagination":{"total":1,"count":1,"skip":0,"limit":200}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":[
					{"id":"sess2","name":"BCK_SIEP-FLORIAN","result":"Warning","state":"Stopped","creationTime":"2026-04-09T01:00:00Z","endTime":"2026-04-09T01:45:00Z"}
				],"pagination":{"total":1,"count":1,"skip":0,"limit":200}}`))
			}
		case r.URL.Path == "/api/v1/backupInfrastructure/repositories":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"repo1","name":"Default Backup Repository","type":"WinLocal","capacityGB":500.0,"freeGB":200.0,"usedSpaceGB":300.0}
			],"pagination":{"total":1,"count":1,"skip":0,"limit":200}}`))
		case r.URL.Path == "/api/v1/license":
			_, _ = w.Write([]byte(`{"type":"Rental","status":"Valid","expirationDate":"2026-12-31T00:00:00Z","instanceLicenseSummary":{"licensedInstancesNumber":85,"usedInstancesNumber":92},"socketLicenseSummary":{"licensedSocketsNumber":0,"usedSocketsNumber":0}}`))
		case r.URL.Path == "/api/v1/backupInfrastructure/proxies":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"proxy1","name":"VMware Backup Proxy","type":"Vi","server":{"name":"proxy-srv"},"isDisabled":false,"maxTaskCount":4}
			],"pagination":{"total":1,"count":1,"skip":0,"limit":200}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Create probe with test server URL
	config := map[string]interface{}{
		"endpoint":       ts.URL,
		"username":       "admin",
		"password":       "secret",
		"verify_ssl":     true,
		"hours_to_check": 8760, // 1 year window to ensure test sessions are always within range
	}
	probe, err := NewVeeamProbe(config, newTestLogger())
	if err != nil {
		t.Fatalf("failed to create probe: %v", err)
	}

	// Inject the test server's TLS client
	vp := probe.(*veeamProbe)
	vp.client = &veeamClient{
		httpClient: ts.Client(),
		endpoint:   ts.URL,
		username:   "admin",
		password:   "secret",
		ctx:        context.Background(),
		logger:     logger.NewModuleLogger(newTestLogger(), "probe.veeam.client"),
	}

	points, err := vp.Collect()
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if len(points) == 0 {
		t.Fatal("expected datapoints, got none")
	}

	// Verify expected metric names are present
	expectedMetrics := []string{
		"veeam_jobs_total",
		"veeam_jobs_success",
		"veeam_jobs_warning",
		"veeam_jobs_failed",
		"veeam_jobs_running",
		"veeam_job_status",
		"veeam_job_duration_min",
		"veeam_job_hours_since",
		"veeam_repo_total_gb",
		"veeam_repo_used_gb",
		"veeam_repo_free_gb",
		"veeam_repo_free_pct",
		"veeam_license_status",
		"veeam_license_days_left",
		"veeam_license_instances_total",
		"veeam_license_instances_used",
		"veeam_license_instances_remaining",
		"veeam_proxy_status",
		"veeam_proxy_max_tasks",
		"veeam_proxies_total",
		"veeam_proxies_enabled",
		"veeam_proxies_disabled",
	}

	foundMetrics := make(map[string]bool)
	for _, dp := range points {
		foundMetrics[dp.Name] = true
	}

	for _, name := range expectedMetrics {
		if !foundMetrics[name] {
			t.Errorf("missing expected metric: %s", name)
		}
	}

	// Verify specific values
	for _, dp := range points {
		switch dp.Name {
		case "veeam_repo_total_gb":
			if dp.Value != 500 {
				t.Errorf("veeam_repo_total_gb = %v, want 500", dp.Value)
			}
		case "veeam_repo_free_gb":
			if dp.Value != 200 {
				t.Errorf("veeam_repo_free_gb = %v, want 200", dp.Value)
			}
		case "veeam_repo_free_pct":
			if dp.Value != 40 {
				t.Errorf("veeam_repo_free_pct = %v, want 40", dp.Value)
			}
		case "veeam_license_status":
			if dp.Value != 0 {
				t.Errorf("veeam_license_status = %v, want 0 (Valid)", dp.Value)
			}
		case "veeam_license_instances_used":
			if dp.Value != 92 {
				t.Errorf("veeam_license_instances_used = %v, want 92", dp.Value)
			}
		case "veeam_license_instances_remaining":
			if dp.Value != 0 {
				t.Errorf("veeam_license_instances_remaining = %v, want 0 (clamped)", dp.Value)
			}
		case "veeam_proxies_total":
			if dp.Value != 1 {
				t.Errorf("veeam_proxies_total = %v, want 1", dp.Value)
			}
		case "veeam_proxies_enabled":
			if dp.Value != 1 {
				t.Errorf("veeam_proxies_enabled = %v, want 1", dp.Value)
			}
		case "veeam_proxy_max_tasks":
			if dp.Value != 4 {
				t.Errorf("veeam_proxy_max_tasks = %v, want 4", dp.Value)
			}
		}
	}

	// Verify tags are present
	verifyTagPresent(t, points, "veeam_job_status", "job_name")
	verifyTagPresent(t, points, "veeam_job_status", "job_type")
	verifyTagPresent(t, points, "veeam_repo_total_gb", "repo_name")
	verifyTagPresent(t, points, "veeam_proxy_status", "proxy_name")
	verifyTagPresent(t, points, "veeam_repo_total_gb", "endpoint")
}

// verifyTagPresent checks that at least one datapoint with the given name has the specified tag key
func verifyTagPresent(t *testing.T, points []datapoint.DataPoint, metricName, tagKey string) {
	t.Helper()
	for _, dp := range points {
		if dp.Name == metricName {
			for _, tag := range dp.Tags {
				if tag.Key == tagKey {
					return
				}
			}
		}
	}
	t.Errorf("metric %q missing tag %q", metricName, tagKey)
}

// --- TestBuildRepositoryMetrics ---

func TestBuildRepositoryMetrics(t *testing.T) {
	repos := []repository{
		{ID: "r1", Name: "Repo1", CapacityGB: 1000, FreeGB: 250, UsedSpaceGB: 750},
		{ID: "r2", Name: "Repo2", CapacityGB: 0, FreeGB: 0, UsedSpaceGB: 0},
	}
	now := time.Now()
	points := buildRepositoryMetrics(repos, now)

	// 4 metrics per repo = 8 total
	if len(points) != 8 {
		t.Errorf("expected 8 datapoints, got %d", len(points))
	}

	// Check zero-capacity repo produces 0% free
	for _, dp := range points {
		if dp.Name == "veeam_repo_free_pct" {
			for _, tag := range dp.Tags {
				if tag.Key == "repo_name" && tag.Value == "Repo2" {
					if dp.Value != 0 {
						t.Errorf("expected 0%% free for zero-capacity repo, got %v", dp.Value)
					}
				}
			}
		}
	}
}

// --- TestBuildLicenseMetrics ---

func TestBuildLicenseMetrics(t *testing.T) {
	lic := &licenseInfo{
		Status:         "Valid",
		ExpirationDate: time.Now().Add(90 * 24 * time.Hour),
		InstanceLicenseSummary: instanceLicenseSummary{
			LicensedInstancesNumber: 100,
			UsedInstancesNumber:     50,
		},
	}
	now := time.Now()
	points := buildLicenseMetrics(lic, now)

	if len(points) != 5 {
		t.Errorf("expected 5 license datapoints, got %d", len(points))
	}

	for _, dp := range points {
		switch dp.Name {
		case "veeam_license_status":
			if dp.Value != 0 {
				t.Errorf("expected status 0 (Valid), got %v", dp.Value)
			}
		case "veeam_license_instances_remaining":
			if dp.Value != 50 {
				t.Errorf("expected 50 remaining instances, got %v", dp.Value)
			}
		}
	}
}

// --- TestBuildProxyMetrics ---

func TestBuildProxyMetrics(t *testing.T) {
	proxies := []proxy{
		{ID: "p1", Name: "Proxy1", IsDisabled: false, MaxTaskCount: 4},
		{ID: "p2", Name: "Proxy2", IsDisabled: true, MaxTaskCount: 2},
	}
	now := time.Now()
	points := buildProxyMetrics(proxies, now)

	// 2 metrics per proxy + 3 aggregates = 7
	if len(points) != 7 {
		t.Errorf("expected 7 proxy datapoints, got %d", len(points))
	}

	for _, dp := range points {
		switch dp.Name {
		case "veeam_proxies_enabled":
			if dp.Value != 1 {
				t.Errorf("expected 1 enabled proxy, got %v", dp.Value)
			}
		case "veeam_proxies_disabled":
			if dp.Value != 1 {
				t.Errorf("expected 1 disabled proxy, got %v", dp.Value)
			}
		}
	}
}

// --- TestBuildJobOverviewMetrics ---

func TestBuildJobOverviewMetrics(t *testing.T) {
	jobs := []job{
		{ID: "j1", Name: "Job1", Type: "HyperVBackup", IsDisabled: false},
		{ID: "j2", Name: "Job2", Type: "HyperVBackup", IsDisabled: false},
		{ID: "j3", Name: "Job3", Type: "WindowsAgentBackup", IsDisabled: false},
		{ID: "j4", Name: "Disabled", Type: "HyperVBackup", IsDisabled: true},
	}
	sessionsByJob := map[string][]session{
		"j1": {{Result: "Success", State: "Stopped"}},
		"j2": {{Result: "Failed", State: "Stopped"}},
		"j3": {{Result: "Warning", State: "Stopped"}},
	}

	now := time.Now()
	points := buildJobOverviewMetrics(jobs, sessionsByJob, 24, now)

	// 2 job types x 5 metrics = 10
	if len(points) != 10 {
		t.Errorf("expected 10 overview datapoints, got %d", len(points))
	}
}

// --- TestBuildJobDetailMetrics ---

func TestBuildJobDetailMetrics(t *testing.T) {
	creationTime := time.Date(2026, 4, 9, 2, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, 4, 9, 3, 30, 0, 0, time.UTC)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	jobs := []job{
		{ID: "j1", Name: "TestJob", Type: "Backup", IsDisabled: false},
	}
	sessionsByJob := map[string][]session{
		"j1": {{
			Result:       "Success",
			State:        "Stopped",
			CreationTime: creationTime,
			EndTime:      endTime,
		}},
	}

	points := buildJobDetailMetrics(jobs, sessionsByJob, 24, now)

	// status + duration + hours_since = 3
	if len(points) != 3 {
		t.Errorf("expected 3 detail datapoints, got %d", len(points))
	}

	for _, dp := range points {
		switch dp.Name {
		case "veeam_job_status":
			if dp.Value != 1 {
				t.Errorf("expected status 1 (Success), got %v", dp.Value)
			}
		case "veeam_job_duration_min":
			if dp.Value != 90 {
				t.Errorf("expected duration 90 min, got %v", dp.Value)
			}
		case "veeam_job_hours_since":
			if dp.Value < 8 || dp.Value > 9 {
				t.Errorf("expected hours_since ~8.5, got %v", dp.Value)
			}
		}
	}
}
