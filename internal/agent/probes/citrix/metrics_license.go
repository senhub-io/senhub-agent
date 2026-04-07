package citrix

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/go-ntlmssp"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// CollectLicenseMetrics collects licensing metrics using a fallback chain:
// 1. Try DDC Sites endpoint (LicensedSessionsActive, PeakConcurrentLicenseUsers, etc.)
// 2. If DDC fields are empty/zero, try License Server direct (port 8083)
// 3. If neither source has data, return nil (no license metrics emitted)
func (mc *MetricsCollector) CollectLicenseMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	// Attempt 1: DDC Sites endpoint
	if mc.ddcClient != nil {
		metrics, err := mc.collectLicenseFromDDC(ctx, timestamp)
		if err == nil && len(metrics) > 0 {
			return metrics, nil
		}
		if err != nil {
			mc.logger.Debug().Err(err).Msg("DDC license info unavailable, trying license server")
		}
	}

	// Attempt 2: License Server direct (port 8083)
	if mc.licenseConfig != nil {
		metrics, err := mc.collectLicenseFromServer(ctx, timestamp)
		if err == nil && len(metrics) > 0 {
			return metrics, nil
		}
		if err != nil {
			mc.logger.Warn().Err(err).Str("url", mc.licenseConfig.URL).Msg("License server query failed")
		}
	}

	// No license data available from any source
	mc.logger.Debug().Msg("No license data source available - skipping license metrics")
	return nil, nil
}

// collectLicenseFromDDC tries to get license usage from the DDC Sites endpoint
func (mc *MetricsCollector) collectLicenseFromDDC(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	licenseInfo, err := mc.ddcClient.GetLicenseInfo(ctx, mc.siteFilter)
	if err != nil {
		return nil, err
	}

	// Check if the DDC actually returned usage data (some CVAD versions don't)
	if licenseInfo.LicensedSessionsActive == 0 &&
		licenseInfo.PeakConcurrentLicenseUsers == 0 &&
		licenseInfo.TotalUniqueLicenseUsers == 0 &&
		!licenseInfo.LicensingGracePeriodActive {
		mc.logger.Debug().Msg("DDC returned zero license usage - fields likely not supported on this CVAD version")
		return nil, nil
	}

	return mc.buildLicenseMetrics(timestamp,
		licenseInfo.LicensedSessionsActive,
		licenseInfo.PeakConcurrentLicenseUsers,
		licenseInfo.TotalUniqueLicenseUsers,
		licenseInfo.LicenseGraceSessionsRemaining,
		licenseInfo.LicensingGracePeriodActive,
		licenseInfo.LicensingGraceHoursLeft,
	), nil
}

// LicenseServerResponse represents the license usage data from the Citrix License Server
type LicenseServerResponse struct {
	LicensesInUse    int    `json:"LicensesInUse"`
	LicensesAvailable int   `json:"LicensesAvailable"`
	LicenseOverdraft int    `json:"LicenseOverdraft"`
	ProductName      string `json:"LocalizedLicenseProductName"`
	LicenseType      string `json:"LicenseType"`
}

// collectLicenseFromServer queries the Citrix License Server directly via its web API.
// Tries all configured URLs (primary + fallbacks) with multiple known API endpoints.
func (mc *MetricsCollector) collectLicenseFromServer(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	// Build list of all license server URLs
	allURLs := []string{strings.TrimSuffix(mc.licenseConfig.URL, "/")}
	for _, fb := range mc.licenseConfig.FallbackURLs {
		allURLs = append(allURLs, strings.TrimSuffix(fb, "/"))
	}

	// Create NTLM-capable HTTP client
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !mc.licenseConfig.VerifySSL, // #nosec G402 - Configurable SSL verification
		},
	}

	client := &http.Client{
		Transport: ntlmssp.Negotiator{RoundTripper: transport},
		Timeout:   15 * time.Second,
	}

	// Try known API endpoints in order of likelihood
	endpoints := []string{
		"/api/1.0/license/usage",
		"/api/license/usage",
		"/api/1.0/licensing/inventory",
		"/api/licensing/inventory",
	}

	for _, serverURL := range allURLs {
		for _, endpoint := range endpoints {
			url := serverURL + endpoint

			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				continue
			}
			req.Header.Set("Accept", "application/json")
			req.SetBasicAuth(mc.licenseConfig.Auth.Username, mc.licenseConfig.Auth.Password)

		resp, err := client.Do(req)
		if err != nil {
			mc.logger.Debug().Err(err).Str("endpoint", endpoint).Msg("License server endpoint unreachable")
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			mc.logger.Debug().
				Int("status", resp.StatusCode).
				Str("endpoint", endpoint).
				Msg("License server endpoint returned non-200")
			continue
		}

		// Try to parse as array of license pools
		var licenses []LicenseServerResponse
		if err := json.Unmarshal(body, &licenses); err != nil {
			// Try single object
			var single LicenseServerResponse
			if err2 := json.Unmarshal(body, &single); err2 != nil {
				mc.logger.Debug().
					Str("endpoint", endpoint).
					Str("body_preview", truncateString(string(body), 500)).
					Msg("License server response not parseable - trying next endpoint")
				continue
			}
			licenses = []LicenseServerResponse{single}
		}

		// Filter out system licenses and aggregate
		var totalInUse, totalAvailable, totalOverdraft int
		for _, lic := range licenses {
			if strings.EqualFold(lic.LicenseType, "SYS") {
				continue
			}
			totalInUse += lic.LicensesInUse
			totalAvailable += lic.LicensesAvailable
			totalOverdraft += lic.LicenseOverdraft
		}

		if totalAvailable == 0 && totalInUse == 0 {
			mc.logger.Debug().Str("endpoint", endpoint).Msg("License server returned zero data")
			continue
		}

		mc.logger.Info().
			Int("in_use", totalInUse).
			Int("available", totalAvailable).
			Int("overdraft", totalOverdraft).
			Str("endpoint", endpoint).
			Msg("License data retrieved from License Server")

		// Map to our standard metrics
		return mc.buildLicenseMetrics(timestamp,
			totalInUse,       // active sessions ≈ licenses in use
			totalInUse,       // peak = current (we don't have historical)
			0,                // unique users not available from this source
			totalAvailable,   // grace sessions ≈ available licenses
			totalOverdraft > 0, // grace active if overdraft
			0,                // grace hours not available
		), nil
		}
	}

	return nil, fmt.Errorf("no working license server endpoint found at %v", allURLs)
}

// buildLicenseMetrics creates the standardized license metric datapoints
func (mc *MetricsCollector) buildLicenseMetrics(timestamp time.Time,
	sessionsActive, peakConcurrent, uniqueUsers, graceSessionsLeft int,
	gracePeriodActive bool, graceHoursLeft int,
) []datapoint.DataPoint {

	licenseTag := tags.Tag{Key: "metric_type", Value: "licensing"}

	var graceActive float32
	if gracePeriodActive {
		graceActive = 1
	}

	return []datapoint.DataPoint{
		{Name: MetricLicenseSessionsActive, Value: float32(sessionsActive), Timestamp: timestamp, Tags: []tags.Tag{licenseTag}},
		{Name: MetricLicensePeakConcurrent, Value: float32(peakConcurrent), Timestamp: timestamp, Tags: []tags.Tag{licenseTag}},
		{Name: MetricLicenseUniqueUsers, Value: float32(uniqueUsers), Timestamp: timestamp, Tags: []tags.Tag{licenseTag}},
		{Name: MetricLicenseGraceSessionsLeft, Value: float32(graceSessionsLeft), Timestamp: timestamp, Tags: []tags.Tag{licenseTag}},
		{Name: MetricLicenseGracePeriodActive, Value: graceActive, Timestamp: timestamp, Tags: []tags.Tag{licenseTag}},
		{Name: MetricLicenseGraceHoursLeft, Value: float32(graceHoursLeft), Timestamp: timestamp, Tags: []tags.Tag{licenseTag}},
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
