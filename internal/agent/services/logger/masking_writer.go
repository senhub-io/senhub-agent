package logger

import (
	"encoding/json"
	"io"
	"strings"
)

// MaskingWriter is an io.Writer that masks sensitive information before writing
type MaskingWriter struct {
	out io.Writer
}

// NewMaskingWriter creates a new writer that masks sensitive data
func NewMaskingWriter(out io.Writer) *MaskingWriter {
	return &MaskingWriter{out: out}
}

// Write implements the io.Writer interface
func (w *MaskingWriter) Write(p []byte) (n int, err error) {
	// Detect if it's valid JSON
	var jsonObj map[string]interface{}
	if json.Unmarshal(p, &jsonObj) == nil {
		// If it's JSON, mask sensitive fields in the structure
		masked := maskJSONFields(jsonObj)
		// Re-encode as JSON
		maskedJSON, err := json.Marshal(masked)
		if err != nil {
			// In case of error, mask the raw string
			maskedStr := MaskSensitiveData(string(p))
			_, writeErr := w.out.Write([]byte(maskedStr))
			if writeErr != nil {
				return 0, writeErr
			}
			return len(p), nil // Return original input length
		}
		_, writeErr := w.out.Write(maskedJSON)
		if writeErr != nil {
			return 0, writeErr
		}
		return len(p), nil // Return original input length
	}
	
	// If it's not JSON, treat as string
	maskedStr := MaskSensitiveData(string(p))
	_, writeErr := w.out.Write([]byte(maskedStr))
	if writeErr != nil {
		return 0, writeErr
	}
	return len(p), nil // Return original input length
}

// maskJSONFields recursively traverses a JSON object and masks sensitive fields
func maskJSONFields(obj map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	
	for key, value := range obj {
		newKey := key
		
		// Mask sensitive fields based on field name
		if isSensitiveFieldName(key) {
			switch v := value.(type) {
			case string:
				result[newKey] = maskValue(v)
				continue
			}
		}
		
		// Recursive processing of nested structures
		switch v := value.(type) {
		case map[string]interface{}:
			result[newKey] = maskJSONFields(v)
		case []interface{}:
			newArray := make([]interface{}, len(v))
			for i, item := range v {
				if mapItem, ok := item.(map[string]interface{}); ok {
					newArray[i] = maskJSONFields(mapItem)
				} else {
					newArray[i] = item
				}
			}
			result[newKey] = newArray
		default:
			result[newKey] = value
		}
	}
	
	return result
}

// isSensitiveFieldName checks if a field name is likely to contain sensitive data
func isSensitiveFieldName(fieldName string) bool {
	sensitiveNames := []string{
		"password", "passwd", "pwd", 
		"token", "api_key", "apikey", 
		"secret", "credential", 
		"authentication_key", "authkey",
		"authorization", "auth",
	}
	
	fieldNameLower := strings.ToLower(fieldName)
	for _, name := range sensitiveNames {
		if strings.Contains(fieldNameLower, name) {
			return true
		}
	}
	
	return false
}