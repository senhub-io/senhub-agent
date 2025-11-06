package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestNewServer(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	server := NewServer("test-key", "https://example.com", baseLogger)
	if server == nil {
		t.Fatal("NewServer() returned nil")
	}

	// Verify it implements Server interface
	_, ok := server.(Server)
	if !ok {
		t.Error("NewServer() does not implement Server interface")
	}
}

func TestServer_NewRequest(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	srv := NewServer("test-auth-key", "https://example.com", baseLogger).(*server)

	tests := []struct {
		name    string
		method  string
		url     string
		body    io.Reader
		wantErr bool
	}{
		{"Valid GET request", "GET", "https://example.com/api", nil, false},
		{"Valid POST request", "POST", "https://example.com/api", strings.NewReader("test"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := srv.NewRequest(tt.method, tt.url, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify auth header is set
				authHeader := req.Header.Get("X-AGENT-KEY")
				if authHeader != "test-auth-key" {
					t.Errorf("Expected X-AGENT-KEY header 'test-auth-key', got '%s'", authHeader)
				}

				// Verify method
				if req.Method != tt.method {
					t.Errorf("Expected method '%s', got '%s'", tt.method, req.Method)
				}
			}
		})
	}
}

func TestServer_Get(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		// Verify auth header
		authKey := r.Header.Get("X-AGENT-KEY")
		if authKey != "test-key-123" {
			t.Errorf("Expected auth key 'test-key-123', got '%s'", authKey)
		}

		// Verify path
		if r.URL.Path != "/api/test" {
			t.Errorf("Expected path '/api/test', got '%s'", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	srv := NewServer("test-key-123", ts.URL, baseLogger)

	resp, err := srv.Get("/api/test")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	expected := `{"status":"ok"}`
	if string(body) != expected {
		t.Errorf("Expected body '%s', got '%s'", expected, string(body))
	}
}

func TestServer_Post(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Verify auth header
		authKey := r.Header.Get("X-AGENT-KEY")
		if authKey != "test-key-456" {
			t.Errorf("Expected auth key 'test-key-456', got '%s'", authKey)
		}

		// Verify content type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
		}

		// Read and verify body
		body, _ := io.ReadAll(r.Body)
		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Errorf("Failed to unmarshal request body: %v", err)
		}

		if data["test"] != "value" {
			t.Errorf("Expected test='value', got '%v'", data["test"])
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"created":"true"}`))
	}))
	defer ts.Close()

	srv := NewServer("test-key-456", ts.URL, baseLogger)

	data := map[string]string{"test": "value"}
	resp, err := srv.Post("/api/create", data)
	if err != nil {
		t.Fatalf("Post() returned error: %v", err)
	}

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}
}

func TestServer_PostStream(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Verify auth header
		authKey := r.Header.Get("X-AGENT-KEY")
		if authKey != "test-key-stream" {
			t.Errorf("Expected auth key 'test-key-stream', got '%s'", authKey)
		}

		// Verify content type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/stream+json" {
			t.Errorf("Expected Content-Type 'application/stream+json', got '%s'", contentType)
		}

		// Read stream body
		body, _ := io.ReadAll(r.Body)
		expected := `{"line1":"data1"}
{"line2":"data2"}`
		if string(body) != expected {
			t.Errorf("Expected stream body '%s', got '%s'", expected, string(body))
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	srv := NewServer("test-key-stream", ts.URL, baseLogger)

	streamData := `{"line1":"data1"}
{"line2":"data2"}`
	resp, err := srv.PostStream("/api/stream", streamData)
	if err != nil {
		t.Fatalf("PostStream() returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", resp.StatusCode)
	}
}

func TestServer_Get_InvalidURL(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	// Server with invalid base URL
	srv := NewServer("test-key", "http://invalid-server-that-does-not-exist-12345.local", baseLogger)

	_, err := srv.Get("/api/test")
	if err == nil {
		t.Error("Get() should return error for invalid server")
	}
}

func TestServer_Post_InvalidData(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	srv := NewServer("test-key", ts.URL, baseLogger)

	// Channels cannot be marshaled to JSON
	invalidData := make(chan int)
	_, err := srv.Post("/api/test", invalidData)
	if err == nil {
		t.Error("Post() should return error for unmarshalable data")
	}
}

func TestServer_Authentication_Missing(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	authReceived := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authKey := r.Header.Get("X-AGENT-KEY")
		if authKey != "" {
			authReceived = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create server with empty auth key
	srv := NewServer("", ts.URL, baseLogger)
	srv.Get("/api/test")

	if authReceived {
		t.Error("Expected no auth header for empty key, but header was received")
	}
}

func TestServer_RetryOnFailure(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	attemptCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":"true"}`))
	}))
	defer ts.Close()

	srv := NewServer("test-key", ts.URL, baseLogger)

	resp, err := srv.Get("/api/test")
	if err != nil {
		t.Fatalf("Get() should succeed after retries, got error: %v", err)
	}
	defer resp.Body.Close()

	if attemptCount < 2 {
		t.Errorf("Expected at least 2 attempts due to retries, got %d", attemptCount)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected final status 200, got %d", resp.StatusCode)
	}
}

func TestServer_PathJoining(t *testing.T) {
	mockArgs := &cliArgs.ParsedArgs{}
	baseLogger := logger.NewLogger(mockArgs)

	receivedPath := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	srv := NewServer("test-key", ts.URL, baseLogger)

	tests := []struct {
		path         string
		expectedPath string
	}{
		{"/api/test", "/api/test"},
		{"api/test", "/api/test"},
		{"/api/test/", "/api/test/"},
	}

	for _, tt := range tests {
		t.Run("Path: "+tt.path, func(t *testing.T) {
			receivedPath = ""
			srv.Get(tt.path)
			if receivedPath != tt.expectedPath {
				t.Errorf("Expected path '%s', got '%s'", tt.expectedPath, receivedPath)
			}
		})
	}
}
