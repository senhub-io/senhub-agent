// senhub-agent/internal/agent/services/data_store/http_web.go
package http

import (
	"net/http"

	"senhub-agent.go/internal/agent/services/logger"
)

// WebInterface handles all web UI related functionality
type WebInterface struct {
	logger   *logger.ModuleLogger
	strategy *HTTPSyncStrategy // Reference to parent strategy for validation
}

// NewWebInterface creates a new web interface handler
func NewWebInterface(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *WebInterface {
	return &WebInterface{
		logger:   logger,
		strategy: strategy,
	}
}

// setNoCacheHeaders sets headers to prevent browser caching of dynamic content
func (w *WebInterface) setNoCacheHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Expires", "0")
}

// Web UI Handlers

// HandleWebDashboard serves the main dashboard interface
func (w *WebInterface) HandleWebDashboard(req *http.Request, writer http.ResponseWriter) {
	agentKey, authenticated := w.strategy.authManager.AuthenticateAndExtract(writer, req)
	if !authenticated {
		return
	}

	// Create asset handler with PRTG status
	prtgEnabled := w.strategy.configManager.IsEndpointEnabled("prtg")
	assetHandler := NewAssetHandlerWithPRTG(agentKey, prtgEnabled)

	// Render the new dashboard template
	templateName := GetTemplateName(req.URL.Path)
	if templateName == "" {
		templateName = "dashboard" // Default to dashboard for root and dashboard paths
	}

	content, err := assetHandler.RenderTemplate(templateName)
	if err != nil {
		w.logger.Error().Err(err).Str("template", templateName).Msg("Failed to render dashboard template")
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.setNoCacheHeaders(writer)
	if _, err := writer.Write([]byte(content)); err != nil {
		w.logger.Error().Err(err).Msg("Failed to write content")
	}
}

// HandleWebExplorer serves the API explorer interface
func (w *WebInterface) HandleWebExplorer(req *http.Request, writer http.ResponseWriter) {
	agentKey, authenticated := w.strategy.authManager.AuthenticateAndExtract(writer, req)
	if !authenticated {
		return
	}

	// Create asset handler
	assetHandler := NewAssetHandler(agentKey)

	// Render API Explorer template
	html, err := assetHandler.RenderTemplate("api-explorer")
	if err != nil {
		w.logger.Error().Err(err).Msg("Failed to render API Explorer template")
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.setNoCacheHeaders(writer)
	if _, err := writer.Write([]byte(html)); err != nil {
		w.logger.Error().Err(err).Msg("Failed to write HTML content")
	}
}

// HandleWebDocs serves the documentation interface
func (w *WebInterface) HandleWebDocs(req *http.Request, writer http.ResponseWriter) {
	agentKey, authenticated := w.strategy.authManager.AuthenticateAndExtract(writer, req)
	if !authenticated {
		return
	}

	// Create asset handler with PRTG status
	prtgEnabled := w.strategy.configManager.IsEndpointEnabled("prtg")
	assetHandler := NewAssetHandlerWithPRTG(agentKey, prtgEnabled)

	// Render the documentation template
	content, err := assetHandler.RenderTemplate("docs")
	if err != nil {
		w.logger.Error().Err(err).Msg("Failed to render docs template")
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.setNoCacheHeaders(writer)
	if _, err := writer.Write([]byte(content)); err != nil {
		w.logger.Error().Err(err).Msg("Failed to write content")
	}
}

// // HandleWebGuide serves the user guide interface - TEMPORARILY DISABLED
// func (w *WebInterface) HandleWebGuide(req *http.Request, writer http.ResponseWriter) {
// 	_, authenticated := w.strategy.authManager.AuthenticateAndExtract(writer, req)
// 	if !authenticated {
// 		return
// 	}
//
// 	// Render guide template
// 	content, err := w.assetHandler.RenderTemplate("guide")
// 	if err != nil {
// 		w.logger.Error().Err(err).Msg("Failed to render guide template")
// 		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
// 		return
// 	}
//
// 	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
// 	if _, err := writer.Write([]byte(content)); err != nil {
// 		w.logger.Error().Err(err).Msg("Failed to write content")
// 	}
// }

// HandleWebAssets serves static assets (CSS, JS, images)
func (w *WebInterface) HandleWebAssets(req *http.Request, writer http.ResponseWriter) {
	agentKey, authenticated := w.strategy.authManager.AuthenticateAndExtract(writer, req)
	if !authenticated {
		return
	}

	// Create asset handler and serve the requested asset
	assetHandler := NewAssetHandler(agentKey)
	assetHandler.ServeAsset(writer, req, req.URL.Path)
}
