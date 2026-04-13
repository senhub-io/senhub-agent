package veeam

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"senhub-agent.go/internal/agent/services/logger"
)

// veeamClient handles HTTP communication with the Veeam Backup & Replication REST API
type veeamClient struct {
	httpClient   *http.Client
	endpoint     string
	username     string
	password     string
	logger       *logger.ModuleLogger
	ctx          context.Context
	token        string
	tokenExpiry  time.Time
	tokenMu      sync.Mutex
}

// newVeeamClient creates a new Veeam REST API client
func newVeeamClient(endpoint, username, password string, verifySSL bool, ctx context.Context, baseLogger *logger.Logger) *veeamClient {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !verifySSL, // #nosec G402 - user-configurable SSL verification
		},
	}

	return &veeamClient{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		endpoint: strings.TrimRight(endpoint, "/"),
		username: username,
		password: password,
		ctx:      ctx,
		logger:   logger.NewModuleLogger(baseLogger, "probe.veeam.client"),
	}
}

// authenticate obtains or refreshes an OAuth2 token from the Veeam API.
// Tokens are cached and refreshed 60 seconds before expiry.
func (c *veeamClient) authenticate() error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Return cached token if still valid (with 60s safety margin)
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return nil
	}

	tokenURL := fmt.Sprintf("%s/api/oauth2/token", c.endpoint)

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", c.username)
	form.Set("password", c.password)

	req, err := http.NewRequestWithContext(c.ctx, "POST", tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("authentication failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.token = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	c.logger.Debug().
		Int("expires_in", tokenResp.ExpiresIn).
		Msg("Veeam API token obtained")

	return nil
}

// doRequest performs an authenticated GET request to the Veeam API
func (c *veeamClient) doRequest(path string) ([]byte, error) {
	if err := c.authenticate(); err != nil {
		return nil, fmt.Errorf("authentication error: %w", err)
	}

	reqURL := fmt.Sprintf("%s%s", c.endpoint, path)

	req, err := http.NewRequestWithContext(c.ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-api-version", "1.3-rev1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed for %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d) for %s: %s", resp.StatusCode, path, string(body))
	}

	// Ensure valid UTF-8: Veeam on Windows may return names with non-UTF8 encoding
	if !utf8.Valid(body) {
		body = sanitizeUTF8(body)
	}

	return body, nil
}

// sanitizeUTF8 replaces invalid UTF-8 bytes with the Unicode replacement character.
func sanitizeUTF8(data []byte) []byte {
	var b strings.Builder
	b.Grow(len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		b.WriteRune(r)
		data = data[size:]
	}
	return []byte(b.String())
}

// GetServerInfo retrieves Veeam server information
func (c *veeamClient) GetServerInfo() (*serverInfo, error) {
	body, err := c.doRequest("/api/v1/serverInfo")
	if err != nil {
		return nil, fmt.Errorf("failed to get server info: %w", err)
	}

	var info serverInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode server info: %w", err)
	}

	return &info, nil
}

// fallbackJobTypes lists EJobType values to query individually when the unfiltered
// /api/v1/jobs endpoint fails (e.g. HTTP 500 caused by HyperVBackup bug).
// Source: Veeam REST API v1.3-rev1 swagger.json EJobType enum.
// HyperVBackup is intentionally excluded — it causes a server-side HTTP 500.
var fallbackJobTypes = []string{
	"VSphereBackup",
	"VSphereReplica",
	"BackupCopy",
	"FileBackupCopy",
	"LegacyBackupCopy",
	"WindowsAgentBackup",
	"LinuxAgentBackup",
	"WindowsAgentBackupWorkstationPolicy",
	"LinuxAgentBackupWorkstationPolicy",
	"WindowsAgentBackupServerPolicy",
	"LinuxAgentBackupServerPolicy",
	"FileBackup",
	"ObjectStorageBackup",
	"CloudDirectorBackup",
	"CloudBackupAzure",
	"CloudBackupAWS",
	"CloudBackupGoogle",
	"EntraIDTenantBackup",
	"EntraIDTenantBackupCopy",
	"EntraIDAuditLogBackup",
	"SureBackupContentScan",
}

// GetJobs retrieves all backup jobs by querying each supported type individually.
// The unfiltered /api/v1/jobs endpoint returns HTTP 500 when the server contains
// HyperVBackup jobs (Veeam REST API bug), so we always query per type.
func (c *veeamClient) GetJobs() ([]job, error) {
	var allJobs []job
	var lastErr error

	for _, jt := range fallbackJobTypes {
		jobs, err := fetchAllPaginated[job](c, fmt.Sprintf("/api/v1/jobs?typeFilter=%s", jt))
		if err != nil {
			c.logger.Debug().Err(err).Str("job_type", jt).Msg("Job type query failed, skipping")
			lastErr = err
			continue
		}
		allJobs = append(allJobs, jobs...)
	}

	if len(allJobs) == 0 && lastErr != nil {
		return nil, fmt.Errorf("failed to get jobs (all type queries failed): %w", lastErr)
	}

	return allJobs, nil
}

// GetSessions retrieves recent sessions for a specific job
func (c *veeamClient) GetSessions(jobID string, limit int) ([]session, error) {
	path := fmt.Sprintf("/api/v1/sessions?jobId=%s&limit=%d", jobID, limit)
	body, err := c.doRequest(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get sessions for job %s: %w", jobID, err)
	}

	var resp paginatedResponse[session]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode sessions: %w", err)
	}

	return resp.Data, nil
}

// GetRepositories retrieves all backup repository states (includes capacity metrics)
func (c *veeamClient) GetRepositories() ([]repository, error) {
	return fetchAllPaginated[repository](c, "/api/v1/backupInfrastructure/repositories/states")
}

// GetLicense retrieves Veeam license information
func (c *veeamClient) GetLicense() (*licenseInfo, error) {
	body, err := c.doRequest("/api/v1/license")
	if err != nil {
		return nil, fmt.Errorf("failed to get license info: %w", err)
	}

	var info licenseInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode license info: %w", err)
	}

	return &info, nil
}

// GetProxies retrieves all backup proxy states (includes isDisabled, isOnline)
func (c *veeamClient) GetProxies() ([]proxy, error) {
	return fetchAllPaginated[proxy](c, "/api/v1/backupInfrastructure/proxies/states")
}

// fetchAllPaginated retrieves all items from a paginated endpoint
func fetchAllPaginated[T any](c *veeamClient, basePath string) ([]T, error) {
	var allItems []T
	skip := 0
	limit := 200

	for {
		separator := "?"
		if strings.Contains(basePath, "?") {
			separator = "&"
		}
		path := fmt.Sprintf("%s%sskip=%d&limit=%d", basePath, separator, skip, limit)

		body, err := c.doRequest(path)
		if err != nil {
			return nil, err
		}

		var resp paginatedResponse[T]
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode paginated response: %w", err)
		}

		allItems = append(allItems, resp.Data...)

		// Safety check: break if API returns zero items to prevent infinite loop
		if resp.Pagination.Count == 0 {
			break
		}

		// Check if we've fetched all items
		if skip+resp.Pagination.Count >= resp.Pagination.Total {
			break
		}
		skip += resp.Pagination.Count
	}

	return allItems, nil
}

// isTokenValid returns true if the current token is still valid (with 60s margin)
func (c *veeamClient) isTokenValid() bool {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.token != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second))
}
