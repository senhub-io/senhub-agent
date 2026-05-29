package citrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"senhub-agent.go/probesdk/cliargs"
	"senhub-agent.go/probesdk/logger"
)

func TestSiteIDCaching(t *testing.T) {
	client := &deliveryControllerClient{}

	if client.cachedSiteID != "" {
		t.Error("Expected empty cached site ID initially")
	}

	client.cachedSiteID = "site-123"
	client.cachedSiteName = "Production"
	client.siteIDCachedAt = time.Now()

	if time.Since(client.siteIDCachedAt) >= siteIDCacheTTL {
		t.Error("Cache should be fresh immediately after setting")
	}

	client.siteIDCachedAt = time.Now().Add(-siteIDCacheTTL - time.Minute)
	if time.Since(client.siteIDCachedAt) < siteIDCacheTTL {
		t.Error("Cache should be stale after TTL")
	}
}

func TestSiteIDCacheTTL(t *testing.T) {
	if siteIDCacheTTL != 10*time.Minute {
		t.Errorf("Expected siteIDCacheTTL to be 10 minutes, got %v", siteIDCacheTTL)
	}
}

// newTestDDCServer creates a fake DDC API server that tracks /cvad/manage/me call count.
// It returns a valid token on /cvad/manage/Tokens and site info on /cvad/manage/me.
func newTestDDCServer(t *testing.T, meCallCount *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/cvad/manage/Tokens":
			resp := CVADTokenResponse{
				Token:     "test-token-123",
				Principal: "TEST\\admin",
				UserId:    "user-1",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}
			json.NewEncoder(w).Encode(resp)

		case "/cvad/manage/me":
			meCallCount.Add(1)
			resp := DDCMeResponse{
				UserId:      "user-1",
				DisplayName: "Test Admin",
				Customers: []DDCCustomer{
					{
						Id: "cust-1",
						Sites: []DDCSite{
							{Id: "site-abc-123", Name: "Production"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)

		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestDDCClient(t *testing.T, serverURL string) *deliveryControllerClient {
	t.Helper()
	args := &cliArgs.ParsedArgs{Env: "test", Verbose: false}
	baseLogger := logger.NewLogger(args)
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.ddc.test")

	return &deliveryControllerClient{
		config: DeliveryControllerConfig{
			URL:     serverURL,
			Timeout: 5 * time.Second,
		},
		httpClient:   &http.Client{Timeout: 5 * time.Second},
		logger:       moduleLogger,
		primaryURL:   serverURL,
		fallbackURLs: nil,
		authConfig: AuthConfig{
			Username: "TEST\\admin",
			Password: "password",
		},
	}
}

func TestGetSiteInfo_CachesAfterFirstCall(t *testing.T) {
	var meCallCount atomic.Int32
	server := newTestDDCServer(t, &meCallCount)
	defer server.Close()

	client := newTestDDCClient(t, server.URL)
	ctx := context.Background()

	// First call should hit the API
	siteID, siteName, err := client.getSiteInfo(ctx, "")
	if err != nil {
		t.Fatalf("First getSiteInfo() failed: %v", err)
	}
	if siteID != "site-abc-123" {
		t.Errorf("Expected site ID 'site-abc-123', got %q", siteID)
	}
	if siteName != "Production" {
		t.Errorf("Expected site name 'Production', got %q", siteName)
	}
	if meCallCount.Load() != 1 {
		t.Errorf("Expected 1 /me call after first getSiteInfo, got %d", meCallCount.Load())
	}

	// Second call should use cache (no additional /me call)
	siteID2, siteName2, err := client.getSiteInfo(ctx, "")
	if err != nil {
		t.Fatalf("Second getSiteInfo() failed: %v", err)
	}
	if siteID2 != siteID || siteName2 != siteName {
		t.Error("Cached values should match first call")
	}
	if meCallCount.Load() != 1 {
		t.Errorf("Expected still 1 /me call (cached), got %d", meCallCount.Load())
	}

	// Third, fourth, fifth calls — simulating a full collection cycle
	for i := 0; i < 3; i++ {
		_, _, err := client.getSiteInfo(ctx, "Production")
		if err != nil {
			t.Fatalf("getSiteInfo() call %d failed: %v", i+3, err)
		}
	}
	if meCallCount.Load() != 1 {
		t.Errorf("Expected still 1 /me call after 5 total getSiteInfo calls, got %d", meCallCount.Load())
	}
}

func TestGetSiteInfo_RefreshesAfterTTL(t *testing.T) {
	var meCallCount atomic.Int32
	server := newTestDDCServer(t, &meCallCount)
	defer server.Close()

	client := newTestDDCClient(t, server.URL)
	ctx := context.Background()

	// First call
	_, _, err := client.getSiteInfo(ctx, "")
	if err != nil {
		t.Fatalf("First getSiteInfo() failed: %v", err)
	}
	if meCallCount.Load() != 1 {
		t.Fatalf("Expected 1 /me call, got %d", meCallCount.Load())
	}

	// Simulate TTL expiry
	client.siteIDCachedAt = time.Now().Add(-siteIDCacheTTL - time.Minute)

	// Next call should refresh
	_, _, err = client.getSiteInfo(ctx, "")
	if err != nil {
		t.Fatalf("getSiteInfo() after TTL failed: %v", err)
	}
	if meCallCount.Load() != 2 {
		t.Errorf("Expected 2 /me calls after TTL expiry, got %d", meCallCount.Load())
	}
}

func TestGetSiteInfo_SiteMismatchWarning(t *testing.T) {
	var meCallCount atomic.Int32
	server := newTestDDCServer(t, &meCallCount)
	defer server.Close()

	client := newTestDDCClient(t, server.URL)
	ctx := context.Background()

	// Request a different site name than what the API returns
	siteID, siteName, err := client.getSiteInfo(ctx, "DifferentSite")
	if err != nil {
		t.Fatalf("getSiteInfo() failed: %v", err)
	}

	// Should still return the user's actual site (security: ignore requested site)
	if siteID != "site-abc-123" {
		t.Errorf("Expected site ID 'site-abc-123', got %q", siteID)
	}
	if siteName != "Production" {
		t.Errorf("Expected site name 'Production' (user's site), got %q", siteName)
	}
}

func TestGetSiteInfo_NoSitesError(t *testing.T) {
	// Server that returns empty customers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cvad/manage/Tokens":
			resp := CVADTokenResponse{
				Token:     "test-token",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}
			json.NewEncoder(w).Encode(resp)
		case "/cvad/manage/me":
			resp := DDCMeResponse{
				UserId:    "user-1",
				Customers: []DDCCustomer{},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestDDCClient(t, server.URL)
	ctx := context.Background()

	_, _, err := client.getSiteInfo(ctx, "")
	if err == nil {
		t.Error("Expected error when user has no sites")
	}
}

func TestGetSiteInfo_EmptySiteIDError(t *testing.T) {
	// Server that returns a site with empty ID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cvad/manage/Tokens":
			resp := CVADTokenResponse{
				Token:     "test-token",
				ExpiresAt: time.Now().Add(1 * time.Hour),
			}
			json.NewEncoder(w).Encode(resp)
		case "/cvad/manage/me":
			resp := DDCMeResponse{
				UserId: "user-1",
				Customers: []DDCCustomer{
					{
						Id: "cust-1",
						Sites: []DDCSite{
							{Id: "", Name: "BadSite"},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestDDCClient(t, server.URL)
	ctx := context.Background()

	_, _, err := client.getSiteInfo(ctx, "")
	if err == nil {
		t.Error("Expected error when site ID is empty")
	}
}
