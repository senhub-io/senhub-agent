// Package server provides HTTP client functionality for server communication
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ybbus/httpretry"
	"senhub-agent.go/internal/agent/services/logger"
)

// Server defines the interface for server communication with retry capabilities
// and authentication handling
type Server interface {
	// Get performs HTTP GET request to specified path
	Get(string) (*http.Response, error)

	// Post sends HTTP POST request with JSON data
	Post(string, any) (*http.Response, error)

	// PostStream sends HTTP POST request with streaming data
	PostStream(string, string) (*http.Response, error)
}

// server implements Server interface
type server struct {
	authenticationKey string         // Key for server authentication
	logger            *logger.Logger // Structured logging
	url               string         // Base server URL
	http              *http.Client   // Retry-enabled HTTP client
}

// NewServer creates server client with automatic retry and auth handling
func NewServer(
	authenticationKey string,
	url string,
	logger *logger.Logger,
) Server {
	localLogger := logger.With().Str("service", "Server").Logger()
	localLogger.Debug().
		Str("url", url).
		Msg("[DEBUG] Creating new server client")
	http := httpretry.NewDefaultClient(
		httpretry.WithMaxRetryCount(3),
	)

	return &server{
		authenticationKey: authenticationKey,
		url:               url,
		logger:            &localLogger,
		http:              http,
	}
}

// NewRequest creates authenticated HTTP request with proper headers
func (s *server) NewRequest(method string, url string, body io.Reader) (*http.Request, error) {
	s.logger.Debug().
		Str("method", method).
		Str("url", url).
		Msg("Creating new request")
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("X-AGENT-KEY", s.authenticationKey)
	return req, nil
}

// Get performs HTTP GET request to the specified path
func (s *server) Get(urlPath string) (*http.Response, error) {
	fullUrl, err := url.JoinPath(s.url, urlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to join URL path: %v", err)
	}

	s.logger.Debug().Str("url", fullUrl).Msg("Making GET request")
	req, err := s.NewRequest("GET", fullUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %v", err)
	}
	return s.http.Do(req)
}

// Post sends JSON data via HTTP POST
func (s *server) Post(urlPath string, data any) (*http.Response, error) {
	fullUrl, err := url.JoinPath(s.url, urlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to join URL path: %v", err)
	}

	requestBody, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("[ERROR] Failed to encode data: %v\n", err)
		return nil, fmt.Errorf("failed to marshal JSON: %v", err)
	}

	s.logger.Debug().Str("url", fullUrl).Msg("Making POST request")
	req, err := s.NewRequest("POST", fullUrl, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST request failed: %v", err)
	}

	defer res.Body.Close()
	return res, nil
}

// PostStream sends streaming data via HTTP POST
func (s *server) PostStream(urlPath string, streamBody string) (*http.Response, error) {
	fullUrl, err := url.JoinPath(s.url, urlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to join URL path: %v", err)
	}

	s.logger.Debug().Str("url", fullUrl).Msg("Making POST stream request")
	req, err := s.NewRequest("POST", fullUrl, strings.NewReader(streamBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create stream request: %v", err)
	}

	req.Header.Set("Content-Type", "application/stream+json")
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stream request failed: %v", err)
	}
	return res, nil
}
