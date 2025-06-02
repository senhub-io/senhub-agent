// senhub-agent/internal/agent/services/data_store/assets.go

// This file handles embedding and serving of static assets for HTTP strategy
// All HTML, CSS, and JavaScript files are embedded here for clean separation

package data_store

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
//go:embed assets/css/*.css
var cssFiles embed.FS

//go:embed assets/js/*.js
var jsFiles embed.FS

//go:embed assets/html/*.html
var htmlFiles embed.FS

//go:embed web/logo-senhubagent.png
var logoFile embed.FS

// Template data structure
type TemplateData struct {
	AgentKey string
}

// AssetHandler provides methods for serving embedded assets
type AssetHandler struct {
	agentKey string
	templates map[string]*template.Template
}

// NewAssetHandler creates a new asset handler
func NewAssetHandler(agentKey string) *AssetHandler {
	handler := &AssetHandler{
		agentKey: agentKey,
		templates: make(map[string]*template.Template),
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
	
	// Add more templates here as needed
	// - admin.html
}

// RenderTemplate renders an HTML template with data
func (ah *AssetHandler) RenderTemplate(name string) (string, error) {
	tmpl, exists := ah.templates[name]
	if !exists {
		return "", fmt.Errorf("template not found: %s", name)
	}
	
	var buf bytes.Buffer
	data := TemplateData{
		AgentKey: ah.agentKey,
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
	w.Header().Set("Cache-Control", "public, max-age=3600") // Cache for 1 hour
	
	// Write content
	w.Write(content)
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