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

func TestJobResultValue(t *testing.T) {
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
		got := jobResultValue(tc.result)
		if got != tc.expected {
			t.Errorf("jobResultValue(%q) = %v, want %v", tc.result, got, tc.expected)
		}
	}
}

func TestBottleneckValue(t *testing.T) {
	if got := bottleneckValue("None"); got != 0 {
		t.Errorf("bottleneckValue(None) = %v, want 0", got)
	}
	if got := bottleneckValue("Source"); got != 1 {
		t.Errorf("bottleneckValue(Source) = %v, want 1", got)
	}
	if got := bottleneckValue("Target"); got != 4 {
		t.Errorf("bottleneckValue(Target) = %v, want 4", got)
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
		case r.URL.Path == "/api/v1/jobs/states":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"7d5a054a","name":"BCK_VMware","type":"VSphereBackup","status":"Inactive","lastResult":"Success","lastRun":"2026-04-09T03:30:00Z","objectsCount":5,"highPriority":false,"progressPercent":0,"workload":"Vm","description":""},
				{"id":"bc48c775","name":"BCK_Agent","type":"WindowsAgentBackup","status":"Inactive","lastResult":"Warning","lastRun":"2026-04-09T01:45:00Z","objectsCount":1,"highPriority":false,"progressPercent":0,"workload":"Server","description":""}
			],"pagination":{"total":2,"count":2,"skip":0,"limit":200}}`))
		case r.URL.Path == "/api/v1/backupInfrastructure/repositories/states":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"repo1","name":"Default Backup Repository","type":"WinLocal","capacityGB":500.0,"freeGB":200.0,"usedSpaceGB":300.0,"isOnline":true,"isOutOfDate":false}
			],"pagination":{"total":1,"count":1,"skip":0,"limit":200}}`))
		case r.URL.Path == "/api/v1/license":
			_, _ = w.Write([]byte(`{"status":"Valid","edition":"Standard","licensedTo":"Test","supportId":"123","autoUpdateEnabled":false,"freeAgentInstanceConsumptionEnabled":false,"cloudConnect":"Disabled","IsMultiSection":false,"proactiveSupportEnabled":false,"type":"Rental","expirationDate":"2026-12-31T00:00:00Z","instanceLicenseSummary":{"licensedInstancesNumber":85,"usedInstancesNumber":92,"newInstancesNumber":0,"rentalInstancesNumber":0},"socketLicenseSummary":{"licensedSocketsNumber":0,"usedSocketsNumber":0,"remainingSocketsNumber":0}}`))
		case r.URL.Path == "/api/v1/backupInfrastructure/proxies/states":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"proxy1","name":"VMware Backup Proxy","type":"ViProxy","hostId":"00000000-0000-0000-0000-000000000000","hostName":"proxy-srv","isDisabled":false,"isOnline":true,"isOutOfDate":false}
			],"pagination":{"total":1,"count":1,"skip":0,"limit":200}}`))
		case r.URL.Path == "/api/v1/backupObjects":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"obj1","name":"VM-PROD-01","type":"VirtualMachine","platformName":"VMware","restorePointsCount":14,"lastRunFailed":false},
				{"id":"obj2","name":"SIEP-FLORIAN","type":"Computer","platformName":"LinuxPhysical","restorePointsCount":7,"lastRunFailed":true}
			],"pagination":{"total":2,"count":2,"skip":0,"limit":200}}`))
		case r.URL.Path == "/api/v1/backupInfrastructure/managedServers":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"srv1","name":"vcenter.local","type":"ViHost","status":"Available","description":""},
				{"id":"srv2","name":"hyperv-node1","type":"HvServer","status":"Unavailable","description":""}
			],"pagination":{"total":2,"count":2,"skip":0,"limit":200}}`))
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

	// Inject the test server's TLS client and set probe type
	vp := probe.(*veeamProbe)
	vp.SetProbeType("veeam")
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
		// Job overview
		"veeam_jobs_total",
		"veeam_jobs_success",
		"veeam_jobs_warning",
		"veeam_jobs_failed",
		"veeam_jobs_running",
		// Job detail
		"veeam_job_status",
		"veeam_job_seconds_since",
		"veeam_job_objects_count",
		// Repos
		"veeam_repo_capacity",
		"veeam_repo_used",
		"veeam_repo_free",
		"veeam_repo_free_pct",
		// License
		"veeam_license_status",
		"veeam_license_days_left",
		"veeam_license_instances_total",
		"veeam_license_instances_used",
		"veeam_license_instances_remaining",
		// Proxies
		"veeam_proxy_status",
		"veeam_proxies_total",
		"veeam_proxies_enabled",
		"veeam_proxies_disabled",
		// Protected objects
		"veeam_object_restore_points",
		"veeam_object_last_run_failed",
		"veeam_objects_total",
		"veeam_objects_failed",
		// Infrastructure
		"veeam_server_status",
		"veeam_servers_total",
		"veeam_servers_available",
		"veeam_servers_unavailable",
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
		case "veeam_repo_capacity":
			// 500 GB = 500 * 1024^3 bytes ≈ 5.37e11
			if dp.Value < 5e11 || dp.Value > 6e11 {
				t.Errorf("veeam_repo_capacity = %v, want ~5.37e11 (500GB)", dp.Value)
			}
		case "veeam_repo_free":
			// 200 GB = 200 * 1024^3 bytes ≈ 2.15e11
			if dp.Value < 2e11 || dp.Value > 2.5e11 {
				t.Errorf("veeam_repo_free = %v, want ~2.15e11 (200GB)", dp.Value)
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
		case "veeam_proxy_status":
			if dp.Value != 2 {
				t.Errorf("veeam_proxy_status = %v, want 2 (enabled+online)", dp.Value)
			}
		}
	}

	// Verify tags are present
	verifyTagPresent(t, points, "veeam_job_status", "job_name")
	verifyTagPresent(t, points, "veeam_job_status", "job_type")
	verifyTagPresent(t, points, "veeam_repo_capacity", "repo_name")
	verifyTagPresent(t, points, "veeam_proxy_status", "proxy_name")
	verifyTagPresent(t, points, "veeam_repo_capacity", "endpoint")
	verifyTagPresent(t, points, "veeam_object_restore_points", "object_name")
	verifyTagPresent(t, points, "veeam_server_status", "server_name")
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
		{ID: "p1", Name: "Proxy1", IsDisabled: false, IsOnline: true},
		{ID: "p2", Name: "Proxy2", IsDisabled: true, IsOnline: false},
	}
	now := time.Now()
	points := buildProxyMetrics(proxies, now)

	// 1 metric per proxy + 3 aggregates = 5
	if len(points) != 5 {
		t.Errorf("expected 5 proxy datapoints, got %d", len(points))
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

// --- TestBuildJobStateOverviewMetrics ---

func TestBuildJobStateOverviewMetrics(t *testing.T) {
	lastRun := time.Now().Add(-1 * time.Hour)
	states := []jobState{
		{ID: "j1", Name: "Job1", Type: "VSphereBackup", Status: "Inactive", LastResult: "Success", LastRun: &lastRun, ObjectsCount: 3},
		{ID: "j2", Name: "Job2", Type: "VSphereBackup", Status: "Inactive", LastResult: "Failed", LastRun: &lastRun, ObjectsCount: 2},
		{ID: "j3", Name: "Job3", Type: "WindowsAgentBackup", Status: "Inactive", LastResult: "Warning", LastRun: &lastRun, ObjectsCount: 1},
		{ID: "j4", Name: "Disabled", Type: "VSphereBackup", Status: "Disabled", LastResult: "None"},
	}

	now := time.Now()
	points := buildJobStateOverviewMetrics(states, 24, now)

	// 2 job types × 7 metrics (total, success, warning, failed, running, stale, never_run)
	if len(points) != 14 {
		t.Errorf("expected 14 overview datapoints, got %d", len(points))
	}
}

// --- TestBuildJobStateDetailMetrics ---

func TestBuildJobStateDetailMetrics(t *testing.T) {
	lastRun := time.Date(2026, 4, 9, 3, 30, 0, 0, time.UTC)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

	states := []jobState{
		{
			ID: "j1", Name: "TestJob", Type: "VSphereBackup",
			Status: "Inactive", LastResult: "Success",
			LastRun: &lastRun, ObjectsCount: 5,
		},
	}

	points := buildJobStateDetailMetrics(states, 24, now)

	// status + seconds_since + objects_count = 3
	if len(points) != 3 {
		t.Errorf("expected 3 detail datapoints, got %d", len(points))
	}

	for _, dp := range points {
		switch dp.Name {
		case "veeam_job_status":
			if dp.Value != 1 {
				t.Errorf("expected status 1 (Success), got %v", dp.Value)
			}
		case "veeam_job_seconds_since":
			// 8.5 hours = 30600 seconds
			if dp.Value < 30000 || dp.Value > 31200 {
				t.Errorf("expected seconds_since ~30600, got %v", dp.Value)
			}
		case "veeam_job_objects_count":
			if dp.Value != 5 {
				t.Errorf("expected objects_count 5, got %v", dp.Value)
			}
		}
	}
}

// --- TestComputeJobStatusValue ---
// Covers the priority rules: Running > NeverRun > Failed > Stale > result.
// Each case is the exact pivot a regression would hit — Failed must keep
// priority over Stale, Running must mask everything, NeverRun (LastRun=nil)
// must take precedence over the Failed branch (no run = nothing to call
// "Failed" on yet).
func TestComputeJobStatusValue(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-1 * time.Hour)    // inside any reasonable window
	ancient := now.Add(-48 * time.Hour)  // outside a 24h window
	hoursWindow := 24

	cases := []struct {
		name    string
		state   jobState
		want    float32
		comment string
	}{
		{
			name:    "running masks any past result",
			state:   jobState{Status: "Running", LastResult: "Failed", LastRun: &ancient},
			want:    jobStatusRunning,
			comment: "active run overrides past failure or staleness",
		},
		{
			name:    "never_run takes precedence over default Failed mapping",
			state:   jobState{Status: "Inactive", LastResult: "None", LastRun: nil},
			want:    jobStatusNeverRun,
			comment: "no LastRun → NeverRun (0), even though jobResultValue(\"None\") would also be 0",
		},
		{
			name:    "failed wins over stale",
			state:   jobState{Status: "Inactive", LastResult: "Failed", LastRun: &ancient},
			want:    jobStatusFailed,
			comment: "an old failure must not be downgraded to a stale warning",
		},
		{
			name:    "stale when last run older than window and not failed",
			state:   jobState{Status: "Inactive", LastResult: "Success", LastRun: &ancient},
			want:    jobStatusStale,
			comment: "fresh success aged out → flag the cadence drift",
		},
		{
			name:    "fresh success returns Success",
			state:   jobState{Status: "Inactive", LastResult: "Success", LastRun: &recent},
			want:    jobStatusSuccess,
			comment: "happy path",
		},
		{
			name:    "fresh warning returns Warning",
			state:   jobState{Status: "Inactive", LastResult: "Warning", LastRun: &recent},
			want:    jobStatusWarning,
			comment: "recent warning is preserved as-is",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeJobStatusValue(tc.state, now, hoursWindow)
			if got != tc.want {
				t.Errorf("status = %v, want %v — %s", got, tc.want, tc.comment)
			}
		})
	}
}

// --- TestBuildJobStateDetailMetrics_AlwaysEmits ---
// Regression test for the bug where jobs aged out of hours_to_check would
// silently disappear from PRTG. Every enabled job must produce at least
// status + seconds_since + objects_count, regardless of how recently it ran.
func TestBuildJobStateDetailMetrics_AlwaysEmits(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	ancient := now.Add(-72 * time.Hour) // way outside a 24h window
	fresh := now.Add(-2 * time.Hour)

	states := []jobState{
		{ID: "stale-job", Name: "Weekly", Type: "VSphereBackup",
			Status: "Inactive", LastResult: "Success", LastRun: &ancient},
		{ID: "never-run", Name: "Pristine", Type: "VSphereBackup",
			Status: "Inactive", LastResult: "None", LastRun: nil},
		{ID: "failed-stale", Name: "Broken", Type: "WindowsAgentBackup",
			Status: "Inactive", LastResult: "Failed", LastRun: &ancient},
		{ID: "fresh-ok", Name: "Daily", Type: "VSphereBackup",
			Status: "Inactive", LastResult: "Success", LastRun: &fresh},
		{ID: "disabled", Name: "Off", Type: "VSphereBackup", Status: "Disabled"},
	}

	points := buildJobStateDetailMetrics(states, 24, now)

	// Expect 4 enabled jobs × 3 always-emitted metrics = 12 datapoints
	// (disabled jobs are skipped; bytes/bottleneck require SessionProgress).
	if len(points) != 12 {
		t.Fatalf("expected 12 datapoints (4 enabled × 3 metrics), got %d", len(points))
	}

	// Verify every expected job produces a status and the value matches the
	// priority rules. Index by job_name for assertion clarity.
	statusByJob := map[string]float32{}
	secondsByJob := map[string]float32{}
	for _, dp := range points {
		var jobName string
		for _, tag := range dp.Tags {
			if tag.Key == "job_name" {
				jobName = tag.Value
			}
		}
		switch dp.Name {
		case "veeam_job_status":
			statusByJob[jobName] = dp.Value
		case "veeam_job_seconds_since":
			secondsByJob[jobName] = dp.Value
		}
	}

	expectations := map[string]float32{
		"Weekly":   jobStatusStale,    // aged out
		"Pristine": jobStatusNeverRun, // LastRun nil
		"Broken":   jobStatusFailed,   // failed wins over stale
		"Daily":    jobStatusSuccess,  // happy path
	}
	for job, want := range expectations {
		if got, ok := statusByJob[job]; !ok {
			t.Errorf("job %q has no veeam_job_status (regression: aged-out jobs must still emit)", job)
		} else if got != want {
			t.Errorf("job %q status = %v, want %v", job, got, want)
		}
	}

	// Never-run job must report seconds_since=-1 sentinel, not 0.
	if secondsByJob["Pristine"] != jobSecondsSinceNeverRun {
		t.Errorf("Pristine seconds_since = %v, want %v (never-run sentinel)",
			secondsByJob["Pristine"], jobSecondsSinceNeverRun)
	}

	// Disabled job must NOT appear in the output.
	if _, present := statusByJob["Off"]; present {
		t.Errorf("disabled job leaked into per-job metrics")
	}
}

// --- TestBuildJobStateOverviewMetrics_StaleBucket ---
// Confirms the overview totals reconcile when jobs are stale: total stays
// equal to the count of enabled jobs (no hidden bucket), and stale-classified
// jobs land in `veeam_jobs_stale` rather than vanishing.
func TestBuildJobStateOverviewMetrics_StaleBucket(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	ancient := now.Add(-72 * time.Hour)
	fresh := now.Add(-2 * time.Hour)

	states := []jobState{
		{Type: "VSphereBackup", Status: "Inactive", LastResult: "Success", LastRun: &ancient}, // stale
		{Type: "VSphereBackup", Status: "Inactive", LastResult: "Success", LastRun: &fresh},   // success
		{Type: "VSphereBackup", Status: "Inactive", LastResult: "Failed", LastRun: &ancient},  // failed (priority over stale)
		{Type: "VSphereBackup", Status: "Inactive", LastResult: "None", LastRun: nil},         // never_run
	}

	points := buildJobStateOverviewMetrics(states, 24, now)

	values := map[string]float32{}
	for _, dp := range points {
		values[dp.Name] = dp.Value
	}

	if values["veeam_jobs_total"] != 4 {
		t.Errorf("total = %v, want 4 (no hidden bucket)", values["veeam_jobs_total"])
	}
	if values["veeam_jobs_stale"] != 1 {
		t.Errorf("stale = %v, want 1", values["veeam_jobs_stale"])
	}
	if values["veeam_jobs_success"] != 1 {
		t.Errorf("success = %v, want 1", values["veeam_jobs_success"])
	}
	if values["veeam_jobs_failed"] != 1 {
		t.Errorf("failed = %v, want 1 (kept priority over stale)", values["veeam_jobs_failed"])
	}
	if values["veeam_jobs_never_run"] != 1 {
		t.Errorf("never_run = %v, want 1", values["veeam_jobs_never_run"])
	}

	// Reconciliation: the per-status buckets must sum to total.
	sum := values["veeam_jobs_success"] + values["veeam_jobs_warning"] +
		values["veeam_jobs_failed"] + values["veeam_jobs_running"] +
		values["veeam_jobs_stale"] + values["veeam_jobs_never_run"]
	if sum != values["veeam_jobs_total"] {
		t.Errorf("buckets sum to %v but total is %v — every enabled job must land in exactly one bucket",
			sum, values["veeam_jobs_total"])
	}
}
