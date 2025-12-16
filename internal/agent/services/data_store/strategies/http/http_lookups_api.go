// senhub-agent/internal/agent/services/data_store/strategies/http/http_lookups_api.go

package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/logger"
)

// LookupsManager handles all lookup-related API endpoints
type LookupsManager struct {
	logger        *logger.ModuleLogger
	strategy      *HTTPSyncStrategy
	prtgGenerator *PRTGLookupGenerator
}

// NewLookupsManager creates a new lookups API manager
func NewLookupsManager(strategy *HTTPSyncStrategy) *LookupsManager {
	return &LookupsManager{
		logger:        strategy.logger,
		strategy:      strategy,
		prtgGenerator: NewPRTGLookupGenerator(strategy.lookupRegistry),
	}
}

// RegisterRoutes registers all lookup-related HTTP routes
func (m *LookupsManager) RegisterRoutes(router *mux.Router) {
	// List all lookups
	router.HandleFunc("/api/{agentkey}/lookups", m.HandleListLookups).Methods("GET")

	// Download all PRTG lookups as ZIP
	router.HandleFunc("/api/{agentkey}/lookups/prtg", m.HandleDownloadAllPRTGLookups).Methods("GET")

	// Download specific PRTG lookup XML
	router.HandleFunc("/api/{agentkey}/lookups/prtg/{lookup_id}", m.HandleDownloadPRTGLookup).Methods("GET")
}

// HandleListLookups lists all available lookups
func (m *LookupsManager) HandleListLookups(w http.ResponseWriter, r *http.Request) {
	_, authenticated := m.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Get all lookups
	lookups := m.strategy.lookupRegistry.GetAllLookups()

	// Build response
	response := LookupsListResponse{
		Lookups: lookups,
		Count:   len(lookups),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		m.logger.Error().Err(err).Msg("Failed to encode lookups list response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleDownloadAllPRTGLookups generates and downloads all PRTG lookup XMLs as a ZIP file
func (m *LookupsManager) HandleDownloadAllPRTGLookups(w http.ResponseWriter, r *http.Request) {
	_, authenticated := m.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	m.logger.Info().Msg("Generating PRTG lookup XMLs ZIP file")

	// Generate all XML files
	xmlFiles, err := m.prtgGenerator.GenerateAllXML()
	if err != nil {
		m.logger.Error().Err(err).Msg("Failed to generate PRTG XMLs")
		http.Error(w, "Failed to generate lookup files", http.StatusInternalServerError)
		return
	}

	// Create ZIP archive in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for lookupID, xmlContent := range xmlFiles {
		filename := m.prtgGenerator.GetFilenameForLookup(lookupID)

		fileWriter, err := zipWriter.Create(filename)
		if err != nil {
			m.logger.Error().Err(err).Str("filename", filename).Msg("Failed to create ZIP entry")
			http.Error(w, "Failed to create ZIP archive", http.StatusInternalServerError)
			return
		}

		if _, err := fileWriter.Write([]byte(xmlContent)); err != nil {
			m.logger.Error().Err(err).Str("filename", filename).Msg("Failed to write XML to ZIP")
			http.Error(w, "Failed to write ZIP content", http.StatusInternalServerError)
			return
		}
	}

	if err := zipWriter.Close(); err != nil {
		m.logger.Error().Err(err).Msg("Failed to close ZIP writer")
		http.Error(w, "Failed to finalize ZIP archive", http.StatusInternalServerError)
		return
	}

	// Set headers for file download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"senhub-prtg-lookups.zip\"")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))

	// Write ZIP content
	if _, err := w.Write(buf.Bytes()); err != nil {
		m.logger.Error().Err(err).Msg("Failed to write ZIP response")
	}

	m.logger.Info().
		Int("files", len(xmlFiles)).
		Int("size_bytes", buf.Len()).
		Msg("PRTG lookups ZIP generated successfully")
}

// HandleDownloadPRTGLookup downloads a specific PRTG lookup XML
func (m *LookupsManager) HandleDownloadPRTGLookup(w http.ResponseWriter, r *http.Request) {
	_, authenticated := m.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	lookupID := vars["lookup_id"]

	// Convert URL parameter back to lookup ID (replace underscores with dots)
	lookupID = strings.ReplaceAll(lookupID, "_", ".")

	m.logger.Debug().Str("lookup_id", lookupID).Msg("Generating PRTG lookup XML")

	// Validate lookup exists
	if !m.strategy.lookupRegistry.HasLookup(lookupID) {
		http.Error(w, "Lookup not found", http.StatusNotFound)
		return
	}

	// Generate XML
	xmlContent, err := m.prtgGenerator.GenerateXML(lookupID)
	if err != nil {
		m.logger.Error().Err(err).Str("lookup_id", lookupID).Msg("Failed to generate PRTG XML")
		http.Error(w, "Failed to generate lookup file", http.StatusInternalServerError)
		return
	}

	// Get filename
	filename := m.prtgGenerator.GetFilenameForLookup(lookupID)

	// Set headers for XML download
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(xmlContent)))

	// Write XML content
	if _, err := w.Write([]byte(xmlContent)); err != nil {
		m.logger.Error().Err(err).Msg("Failed to write XML response")
	}

	m.logger.Info().
		Str("lookup_id", lookupID).
		Str("filename", filename).
		Msg("PRTG lookup XML generated successfully")
}

// LookupsListResponse represents the response for /lookups endpoint
type LookupsListResponse struct {
	Lookups map[string]LookupDefinition `json:"lookups"`
	Count   int                         `json:"count"`
}
