// senhub-agent/internal/agent/services/data_store/http_server.go
package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/logger"
)

// ServerManager handles HTTP server lifecycle and routing configuration
type ServerManager struct {
	logger   *logger.ModuleLogger
	strategy *HTTPSyncStrategy // Reference to parent strategy for access to modules
	server   *http.Server
	handlers *HTTPHandlers
}

// NewServerManager creates a new HTTP server manager
func NewServerManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *ServerManager {
	return &ServerManager{
		logger:   logger,
		strategy: strategy,
		handlers: NewHTTPHandlers(strategy),
	}
}

// Start initializes and starts the HTTP server
func (s *ServerManager) Start() error {
	s.logger.Info().
		Int("port", s.strategy.port).
		Str("bind_address", s.strategy.bindAddress).
		Msg("Starting HTTP server")

	// Start cache cleanup goroutine
	s.strategy.cache.StartCleanupRoutine()

	// Setup HTTP routes
	router := s.setupRoutes()

	// Create HTTP server instance
	s.server = s.createHTTPServer(router)

	// Start server in goroutine
	go s.startServerAsync()

	return nil
}

// Shutdown gracefully stops the HTTP server and cleanup routines
func (s *ServerManager) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down HTTP server")

	// Stop cache cleanup
	s.strategy.cache.Stop()

	// Shutdown HTTP server
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}

	return nil
}

// createHTTPServer creates and configures the HTTP server instance
func (s *ServerManager) createHTTPServer(router *mux.Router) *http.Server {
	address := fmt.Sprintf("%s:%d", s.strategy.bindAddress, s.strategy.port)

	return &http.Server{
		Addr:         address,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

// startServerAsync starts the HTTP/HTTPS server in a goroutine
func (s *ServerManager) startServerAsync() {
	address := s.server.Addr

	if s.strategy.configManager.IsTLSEnabled() {
		s.startHTTPSServer(address)
	} else {
		s.startHTTPServer(address)
	}
}

// startHTTPSServer starts the HTTPS server with TLS configuration
func (s *ServerManager) startHTTPSServer(address string) {
	// Get certificate paths from configuration (absolute paths generated during installation)
	certFile := s.strategy.configManager.GetTLSCertFile()
	keyFile := s.strategy.configManager.GetTLSKeyFile()
	
	// Fallback to relative paths if not configured (for backward compatibility)
	if certFile == "" {
		certFile = "./certs/agent-cert.pem"
	}
	if keyFile == "" {
		keyFile = "./certs/agent-key.pem"
	}

	s.logger.Info().
		Str("address", address).
		Int("port", s.strategy.port).
		Str("bind_address", s.strategy.bindAddress).
		Bool("tls_enabled", true).
		Str("cert_file", certFile).
		Str("key_file", keyFile).
		Str("min_tls_version", s.strategy.configManager.GetTLSMinVersion()).
		Msg("HTTPS server listening")

	if err := s.server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
		s.logger.Error().Err(err).Msg("HTTPS server error")
	}
}

// startHTTPServer starts the HTTP server without TLS
func (s *ServerManager) startHTTPServer(address string) {
	s.logger.Info().
		Str("address", address).
		Int("port", s.strategy.port).
		Str("bind_address", s.strategy.bindAddress).
		Bool("tls_enabled", false).
		Msg("HTTP server listening")

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.logger.Error().Err(err).Msg("HTTP server error")
	}
}

// setupRoutes configures HTTP routes using the handlers
func (s *ServerManager) setupRoutes() *mux.Router {
	return s.handlers.SetupRoutes()
}

// GetServer returns the HTTP server instance for external access
func (s *ServerManager) GetServer() *http.Server {
	return s.server
}

// IsRunning checks if the server is currently running
func (s *ServerManager) IsRunning() bool {
	return s.server != nil
}

// GetServerAddress returns the full server address (host:port)
func (s *ServerManager) GetServerAddress() string {
	if s.server != nil {
		return s.server.Addr
	}
	return fmt.Sprintf("%s:%d", s.strategy.bindAddress, s.strategy.port)
}

// GetServerConfig returns server configuration details
func (s *ServerManager) GetServerConfig() map[string]interface{} {
	config := map[string]interface{}{
		"address":       s.GetServerAddress(),
		"port":          s.strategy.port,
		"bind_address":  s.strategy.bindAddress,
		"tls_enabled":   s.strategy.configManager.IsTLSEnabled(),
		"read_timeout":  "10s",
		"write_timeout": "10s",
		"idle_timeout":  "60s",
	}

	if s.strategy.configManager.IsTLSEnabled() {
		config["tls_min_version"] = s.strategy.configManager.GetTLSMinVersion()
		
		// Use configured certificate paths (absolute) or fallback to relative
		certFile := s.strategy.configManager.GetTLSCertFile()
		keyFile := s.strategy.configManager.GetTLSKeyFile()
		if certFile == "" {
			certFile = "./certs/agent-cert.pem"
		}
		if keyFile == "" {
			keyFile = "./certs/agent-key.pem"
		}
		config["cert_file"] = certFile
		config["key_file"] = keyFile
	}

	return config
}

// UpdateServerConfig allows runtime configuration updates (requires restart)
func (s *ServerManager) UpdateServerConfig(newConfig map[string]interface{}) error {
	// Validate configuration through ConfigurationManager
	if err := s.strategy.configManager.UpdateConfiguration(newConfig); err != nil {
		return fmt.Errorf("invalid server configuration: %w", err)
	}

	// Update strategy fields
	s.strategy.port = s.strategy.configManager.GetPort()
	s.strategy.bindAddress = s.strategy.configManager.GetBindAddress()

	s.logger.Info().
		Int("new_port", s.strategy.port).
		Str("new_bind_address", s.strategy.bindAddress).
		Msg("Server configuration updated (restart required)")

	return nil
}

// GetServerStats returns server runtime statistics
func (s *ServerManager) GetServerStats() map[string]interface{} {
	stats := map[string]interface{}{
		"running":     s.IsRunning(),
		"address":     s.GetServerAddress(),
		"tls_enabled": s.strategy.configManager.IsTLSEnabled(),
	}

	if s.server != nil {
		stats["server_configured"] = true
	} else {
		stats["server_configured"] = false
	}

	return stats
}
