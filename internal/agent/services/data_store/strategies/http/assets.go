// senhub-agent/internal/agent/services/data_store/strategies/http/assets.go

// This file handles embedding and serving of static assets for HTTP strategy
// All HTML, CSS, and JavaScript files are embedded here for clean separation

package http

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"text/template"
)

// Embed all assets
//
//go:embed assets/css/*.css
var cssFiles embed.FS

//go:embed assets/js/*.js
var jsFiles embed.FS

//go:embed assets/html/*.html
var htmlFiles embed.FS

//go:embed web/logo-senhubagent.png
var logoFile embed.FS

//go:embed assets/USER_GUIDE.md
var markdownFiles embed.FS

// Template data structure
type TemplateData struct {
	AgentKey    string
	PRTGEnabled bool
}

// AssetHandler provides methods for serving embedded assets
type AssetHandler struct {
	agentKey    string
	prtgEnabled bool
	templates   map[string]*template.Template
}

// NewAssetHandler creates a new asset handler
func NewAssetHandler(agentKey string) *AssetHandler {
	handler := &AssetHandler{
		agentKey:    agentKey,
		prtgEnabled: false, // Default to false, will be set by caller if needed
		templates:   make(map[string]*template.Template),
	}

	// Parse all HTML templates
	handler.parseTemplates()

	return handler
}

// NewAssetHandlerWithPRTG creates a new asset handler with PRTG status
func NewAssetHandlerWithPRTG(agentKey string, prtgEnabled bool) *AssetHandler {
	handler := &AssetHandler{
		agentKey:    agentKey,
		prtgEnabled: prtgEnabled,
		templates:   make(map[string]*template.Template),
	}

	// Parse all HTML templates
	handler.parseTemplates()

	return handler
}

// parseTemplates loads and parses all HTML templates
func (ah *AssetHandler) parseTemplates() {
	// Parse Dashboard template
	if tmplContent, err := htmlFiles.ReadFile("assets/html/dashboard.html"); err == nil {
		if tmpl, err := template.New("dashboard").Parse(string(tmplContent)); err == nil {
			ah.templates["dashboard"] = tmpl
		}
	}

	// Parse API Explorer template
	if tmplContent, err := htmlFiles.ReadFile("assets/html/api-explorer.html"); err == nil {
		if tmpl, err := template.New("api-explorer").Parse(string(tmplContent)); err == nil {
			ah.templates["api-explorer"] = tmpl
		}
	}

	// Parse Documentation template
	if tmplContent, err := htmlFiles.ReadFile("assets/html/docs.html"); err == nil {
		if tmpl, err := template.New("docs").Parse(string(tmplContent)); err == nil {
			ah.templates["docs"] = tmpl
		}
	}

	// Parse Guide template
	if tmplContent, err := htmlFiles.ReadFile("assets/html/guide.html"); err == nil {
		if tmpl, err := template.New("guide").Parse(string(tmplContent)); err == nil {
			ah.templates["guide"] = tmpl
		}
	}
}

// RenderTemplate renders an HTML template with data
func (ah *AssetHandler) RenderTemplate(name string) (string, error) {
	tmpl, exists := ah.templates[name]
	if !exists {
		return "", fmt.Errorf("template not found: %s", name)
	}

	var buf bytes.Buffer
	data := TemplateData{
		AgentKey:    ah.agentKey,
		PRTGEnabled: ah.prtgEnabled,
	}

	err := tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}

	return buf.String(), nil
}

// ServeAsset serves a static asset (CSS, JS, etc.)
func (ah *AssetHandler) ServeAsset(w http.ResponseWriter, r *http.Request, assetPath string) {
	// Remove the /web/{agentkey}/assets/ prefix to get the actual file path
	filePath := strings.TrimPrefix(assetPath, "/web/"+ah.agentKey+"/assets/")

	var content []byte
	var contentType string
	var err error

	// Determine asset type and read from appropriate embed.FS
	switch {
	case strings.HasPrefix(filePath, "css/"):
		content, err = cssFiles.ReadFile("assets/" + filePath)
		contentType = "text/css"

	case strings.HasPrefix(filePath, "js/"):
		content, err = jsFiles.ReadFile("assets/" + filePath)
		contentType = "application/javascript"

	case strings.HasSuffix(filePath, "logo.png"):
		// Serve the embedded logo file
		content, err = logoFile.ReadFile("web/logo-senhubagent.png")
		contentType = "image/png"

	case strings.HasSuffix(filePath, "USER_GUIDE.md"):
		// Serve the user guide markdown file
		content, err = markdownFiles.ReadFile("assets/USER_GUIDE.md")
		contentType = "text/markdown; charset=utf-8"

	default:
		http.NotFound(w, r)
		return
	}

	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate") // No cache for embedded assets

	// Write content
	if _, err := w.Write(content); err != nil {
		// Error writing content - response may be incomplete
		// Note: cannot return error from this point as headers are already sent
		// Silent failure is intentional for network errors to avoid log spam
		_ = err // Explicitly ignore error
	}
}

// GetAvailableAssets returns a list of all available assets for debugging
func (ah *AssetHandler) GetAvailableAssets() map[string][]string {
	assets := make(map[string][]string)

	// List CSS files
	assets["css"] = ah.listFiles(cssFiles, "assets/css")

	// List JS files
	assets["js"] = ah.listFiles(jsFiles, "assets/js")

	// List HTML templates
	assets["html"] = ah.listFiles(htmlFiles, "assets/html")

	return assets
}

// listFiles lists all files in an embedded filesystem
func (ah *AssetHandler) listFiles(efs embed.FS, dir string) []string {
	var files []string

	err := fs.WalkDir(efs, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			// Remove the directory prefix for cleaner output
			relPath := strings.TrimPrefix(path, dir+"/")
			files = append(files, relPath)
		}

		return nil
	})

	if err != nil {
		return []string{fmt.Sprintf("error reading directory: %v", err)}
	}

	return files
}

// GetTemplateName gets the template name from a URL path
func GetTemplateName(urlPath string) string {
	switch {
	case strings.Contains(urlPath, "/dashboard") || strings.HasSuffix(urlPath, "/"):
		return "dashboard"
	case strings.Contains(urlPath, "/explorer"):
		return "api-explorer"
	case strings.Contains(urlPath, "/docs"):
		return "docs"
	case strings.Contains(urlPath, "/admin"):
		return "admin"
	default:
		return ""
	}
}

// IsAssetRequest checks if the request is for a static asset
func IsAssetRequest(path string) bool {
	return strings.Contains(path, "/assets/") &&
		(strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".js") ||
			strings.HasSuffix(path, ".png") ||
			strings.HasSuffix(path, ".jpg") ||
			strings.HasSuffix(path, ".svg"))
}

// GetTemplateHelpers returns template helper functions
func (ah *AssetHandler) GetTemplateHelpers() template.FuncMap {
	return template.FuncMap{
		"formatUptime": func(seconds int64) string {
			if seconds < 60 {
				return fmt.Sprintf("%d seconds", seconds)
			}
			minutes := seconds / 60
			if minutes < 60 {
				return fmt.Sprintf("%d minutes", minutes)
			}
			hours := minutes / 60
			if hours < 24 {
				return fmt.Sprintf("%d hours", hours)
			}
			days := hours / 24
			return fmt.Sprintf("%d days", days)
		},
	}
}
