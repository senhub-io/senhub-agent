// extract_templates.go - Script to safely extract HTML templates from strategy_http.go
// Usage: go run extract_templates.go

package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type TemplateExtraction struct {
	Name       string
	StartLine  int
	EndLine    int
	Content    []string
}

func main() {
	// Read the strategy_http.go file
	file, err := os.Open("internal/agent/services/data_store/strategy_http.go")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Find template functions
	templates := findTemplateFunctions(lines)
	
	fmt.Printf("Found %d template functions:\n", len(templates))
	
	// Debug: search for known function names
	fmt.Println("Debug - Looking for known functions...")
	for i, line := range lines {
		if strings.Contains(line, "generateAPIExplorerHTML") ||
		   strings.Contains(line, "generateDocsHTML") ||
		   strings.Contains(line, "generateAdminHTML") {
			fmt.Printf("Found function at line %d: %s\n", i+1, strings.TrimSpace(line))
		}
	}
	for _, tmpl := range templates {
		fmt.Printf("- %s (lines %d-%d, %d lines)\n", 
			tmpl.Name, tmpl.StartLine+1, tmpl.EndLine+1, len(tmpl.Content))
		
		// Extract and save to separate file
		extractToFile(tmpl)
	}
}

func findTemplateFunctions(lines []string) []TemplateExtraction {
	var templates []TemplateExtraction
	
	// Pattern to find template functions
	funcPattern := regexp.MustCompile(`func.*generate.*HTML.*string.*{`)
	returnPattern := regexp.MustCompile(`return\s*` + "`" + `<!DOCTYPE html>`)
	
	for i, line := range lines {
		if funcPattern.MatchString(line) {
			// Found a template function, find its content
			name := extractFunctionName(line)
			if name != "" {
				// Find the start of the template (return `<!DOCTYPE html>)
				templateStart := -1
				for j := i; j < len(lines) && j < i+10; j++ {
					if returnPattern.MatchString(lines[j]) {
						templateStart = j
						break
					}
				}
				
				if templateStart >= 0 {
					// Find the end of the template
					templateEnd := findTemplateEnd(lines, templateStart)
					if templateEnd >= 0 {
						content := extractTemplateContent(lines, templateStart, templateEnd)
						templates = append(templates, TemplateExtraction{
							Name:      name,
							StartLine: templateStart,
							EndLine:   templateEnd,
							Content:   content,
						})
					}
				}
			}
		}
	}
	
	return templates
}

func extractFunctionName(line string) string {
	// Extract function name from "func (h *HTTPSyncStrategy) generateXXXHTML() string {"
	re := regexp.MustCompile(`generate([A-Z][a-zA-Z]*)HTML`)
	matches := re.FindStringSubmatch(line)
	if len(matches) > 1 {
		return strings.ToLower(matches[1])
	}
	return ""
}

func findTemplateEnd(lines []string, start int) int {
	// Find the end of the template by looking for </html>` followed by }
	inTemplate := false
	
	for i := start; i < len(lines); i++ {
		line := lines[i]
		
		// Check if we're entering the template
		if strings.Contains(line, "return `") {
			inTemplate = true
			continue
		}
		
		if inTemplate {
			// Look for the end pattern
			if strings.Contains(line, "</html>`") {
				// Next line should be the closing brace
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "}" {
					return i+1
				}
			}
		}
	}
	
	return -1
}

func extractTemplateContent(lines []string, start, end int) []string {
	var content []string
	
	for i := start; i <= end; i++ {
		content = append(content, lines[i])
	}
	
	return content
}

func extractToFile(tmpl TemplateExtraction) {
	// Create the templates directory if it doesn't exist
	os.MkdirAll("internal/agent/services/data_store/templates", 0755)
	
	filename := fmt.Sprintf("internal/agent/services/data_store/templates/%s.html", tmpl.Name)
	
	// Process the content to extract just the HTML
	htmlContent := processTemplateContent(tmpl.Content)
	
	// Write to file
	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating %s: %v\n", filename, err)
		return
	}
	defer file.Close()
	
	for _, line := range htmlContent {
		file.WriteString(line + "\n")
	}
	
	fmt.Printf("✅ Extracted %s to %s\n", tmpl.Name, filename)
}

func processTemplateContent(content []string) []string {
	var htmlLines []string
	inTemplate := false
	
	for _, line := range content {
		// Skip the Go function parts
		if strings.Contains(line, "return `") {
			// Extract just the HTML part after the backtick
			parts := strings.Split(line, "`")
			if len(parts) > 1 {
				htmlLines = append(htmlLines, parts[1])
			}
			inTemplate = true
			continue
		}
		
		if inTemplate {
			// Check for end of template
			if strings.Contains(line, "</html>`") {
				// Extract just the HTML part before the backtick
				parts := strings.Split(line, "`")
				if len(parts) > 0 {
					htmlLines = append(htmlLines, parts[0])
				}
				break
			} else {
				// Regular HTML line
				htmlLines = append(htmlLines, line)
			}
		}
	}
	
	return htmlLines
}