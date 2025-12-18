// senhub-agent/internal/agent/services/data_store/strategies/http/http_lookups_api_test.go
package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
)

// createTestHTTPStrategy creates a test HTTP strategy with lookups
func createTestHTTPStrategy(t *testing.T) *HTTPSyncStrategy {
	// Create logger
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	baseLogger := logger.NewLogger(args)

	// Create agent config
	agentConfig := configuration.NewAgentConfiguration(
		"test-agent-key",
		"http://test-server.com",
		baseLogger,
	)

	// Create strategy with empty params
	params := map[string]interface{}{}
	strategyInterface := NewHTTPSyncStrategy(agentConfig, params, baseLogger)
	strategy, ok := strategyInterface.(*HTTPSyncStrategy)
	if !ok {
		t.Fatal("Failed to cast strategy to *HTTPSyncStrategy")
	}

	return strategy
}

// TestLookupsManager_HandleListLookups tests the /lookups endpoint
func TestLookupsManager_HandleListLookups(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	// Create router and register routes
	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	// Test valid request
	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Check response status
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check content type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Parse response
	var response LookupsListResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response structure
	if response.Count == 0 {
		t.Error("Expected at least one lookup, got 0")
	}

	if len(response.Lookups) != response.Count {
		t.Errorf("Count mismatch: count=%d, len(lookups)=%d", response.Count, len(response.Lookups))
	}

	// Verify lookups have expected structure
	for id, lookup := range response.Lookups {
		if lookup.ID != id {
			t.Errorf("Lookup ID mismatch: key=%s, lookup.ID=%s", id, lookup.ID)
		}

		if lookup.Description == "" {
			t.Errorf("Lookup %s has empty description", id)
		}

		if len(lookup.Mappings) == 0 {
			t.Errorf("Lookup %s has no mappings", id)
		}
	}
}

// TestLookupsManager_HandleListLookups_InvalidAuth tests authentication
func TestLookupsManager_HandleListLookups_InvalidAuth(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	// Test with invalid agent key
	req := httptest.NewRequest("GET", "/api/wrong-key/lookups", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 401 Unauthorized
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestLookupsManager_HandleDownloadAllPRTGLookups tests ZIP download
func TestLookupsManager_HandleDownloadAllPRTGLookups(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	// Test valid request
	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups/prtg", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Check response status
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check content type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/zip" {
		t.Errorf("Expected Content-Type 'application/zip', got '%s'", contentType)
	}

	// Check Content-Disposition header
	disposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") {
		t.Error("Content-Disposition should contain 'attachment'")
	}
	if !strings.Contains(disposition, "senhub-prtg-lookups.zip") {
		t.Error("Content-Disposition should contain filename 'senhub-prtg-lookups.zip'")
	}

	// Check Content-Length header exists
	contentLength := w.Header().Get("Content-Length")
	if contentLength == "" {
		t.Error("Content-Length header should be set")
	}

	// Verify ZIP file is valid
	zipReader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("Invalid ZIP file: %v", err)
	}

	// Check ZIP contains XML files
	if len(zipReader.File) == 0 {
		t.Error("ZIP file should contain at least one XML file")
	}

	// Verify each file in ZIP
	for _, file := range zipReader.File {
		// Check filename format
		if !strings.HasPrefix(file.Name, "prtg.valuelookup.") {
			t.Errorf("File %s should start with 'prtg.valuelookup.'", file.Name)
		}

		if !strings.HasSuffix(file.Name, ".ovl") {
			t.Errorf("File %s should have .ovl extension", file.Name)
		}

		// Read and verify XML content
		rc, err := file.Open()
		if err != nil {
			t.Errorf("Failed to open file %s: %v", file.Name, err)
			continue
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Errorf("Failed to read file %s: %v", file.Name, err)
			continue
		}

		// Verify XML header
		xmlContent := string(content)
		if !strings.HasPrefix(xmlContent, `<?xml version="1.0" encoding="UTF-8"?>`) {
			t.Errorf("File %s should start with XML declaration", file.Name)
		}

		// Verify XML contains ValueLookup element
		if !strings.Contains(xmlContent, "<ValueLookup") {
			t.Errorf("File %s should contain <ValueLookup> element", file.Name)
		}
	}
}

// TestLookupsManager_HandleDownloadAllPRTGLookups_InvalidAuth tests ZIP auth
func TestLookupsManager_HandleDownloadAllPRTGLookups_InvalidAuth(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/wrong-key/lookups/prtg", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestLookupsManager_HandleDownloadPRTGLookup tests single XML download
func TestLookupsManager_HandleDownloadPRTGLookup(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	// Test downloading netscaler.lbvserver.state lookup
	// URL parameter uses underscores instead of dots
	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups/prtg/netscaler_lbvserver_state", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Check response status
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check content type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/xml" {
		t.Errorf("Expected Content-Type 'application/xml', got '%s'", contentType)
	}

	// Check Content-Disposition header
	disposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") {
		t.Error("Content-Disposition should contain 'attachment'")
	}
	if !strings.Contains(disposition, ".ovl") {
		t.Error("Content-Disposition should contain .ovl filename")
	}

	// Check Content-Length header exists
	contentLength := w.Header().Get("Content-Length")
	if contentLength == "" {
		t.Error("Content-Length header should be set")
	}

	// Verify XML content
	xmlContent := w.Body.String()

	// Check XML header
	if !strings.HasPrefix(xmlContent, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("XML should start with XML declaration")
	}

	// Check lookup ID (dots, not underscores)
	if !strings.Contains(xmlContent, `id="netscaler.lbvserver.state"`) {
		t.Error("XML should contain lookup ID with dots")
	}

	// Check desired value
	if !strings.Contains(xmlContent, `desiredValue="7"`) {
		t.Error("XML should contain desiredValue=7 for UP state")
	}

	// Check contains state entries
	if !strings.Contains(xmlContent, "<SingleInt") {
		t.Error("XML should contain <SingleInt> entries")
	}
}

// TestLookupsManager_HandleDownloadPRTGLookup_NotFound tests 404 error
func TestLookupsManager_HandleDownloadPRTGLookup_NotFound(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups/prtg/nonexistent_lookup", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 404 Not Found
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}

	// Check error message
	body := w.Body.String()
	if !strings.Contains(body, "Lookup not found") {
		t.Error("Error message should mention 'Lookup not found'")
	}
}

// TestLookupsManager_HandleDownloadPRTGLookup_InvalidAuth tests XML auth
func TestLookupsManager_HandleDownloadPRTGLookup_InvalidAuth(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/wrong-key/lookups/prtg/netscaler_lbvserver_state", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestLookupsManager_RegisterRoutes tests route registration
func TestLookupsManager_RegisterRoutes(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	// Test that routes are registered by making requests
	testCases := []struct {
		name            string
		path            string
		method          string
		allowedStatuses []int
	}{
		{"List lookups", "/api/test-agent-key/lookups", "GET", []int{http.StatusOK}},
		{"Download all PRTG", "/api/test-agent-key/lookups/prtg", "GET", []int{http.StatusOK}},
		{"Download single PRTG", "/api/test-agent-key/lookups/prtg/netscaler_lbvserver_state", "GET", []int{http.StatusOK}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			// Check if status is one of the allowed statuses
			statusAllowed := false
			for _, allowedStatus := range tc.allowedStatuses {
				if w.Code == allowedStatus {
					statusAllowed = true
					break
				}
			}

			if !statusAllowed {
				t.Errorf("Route %s %s returned unexpected status %d", tc.method, tc.path, w.Code)
			}
		})
	}
}

// TestLookupsListResponse_Structure tests response structure
func TestLookupsListResponse_Structure(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	var response LookupsListResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response has both fields
	if response.Lookups == nil {
		t.Error("Response should have 'lookups' field")
	}

	// Count should match map length
	if response.Count != len(response.Lookups) {
		t.Errorf("Count mismatch: count=%d, map_length=%d", response.Count, len(response.Lookups))
	}

	// Verify at least netscaler lookups are present
	netscalerLookupFound := false
	for id := range response.Lookups {
		if strings.HasPrefix(id, "netscaler.") {
			netscalerLookupFound = true
			break
		}
	}

	if !netscalerLookupFound {
		t.Error("Response should contain at least one netscaler lookup")
	}
}

// TestNewLookupsManager tests manager creation
func TestNewLookupsManager(t *testing.T) {
	strategy := createTestHTTPStrategy(t)
	manager := NewLookupsManager(strategy)

	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}

	if manager.logger == nil {
		t.Error("Manager should have logger")
	}

	if manager.strategy == nil {
		t.Error("Manager should have strategy reference")
	}

	if manager.prtgGenerator == nil {
		t.Error("Manager should have PRTG generator")
	}
}

// BenchmarkHandleListLookups benchmarks the list lookups endpoint
func BenchmarkHandleListLookups(b *testing.B) {
	strategy := createTestHTTPStrategy(&testing.T{})
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkHandleDownloadPRTGLookup benchmarks single XML download
func BenchmarkHandleDownloadPRTGLookup(b *testing.B) {
	strategy := createTestHTTPStrategy(&testing.T{})
	manager := NewLookupsManager(strategy)

	router := mux.NewRouter()
	manager.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/test-agent-key/lookups/prtg/netscaler_lbvserver_state", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
