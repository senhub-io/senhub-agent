// Package validators provides validation utilities for URLs and durations
package validators

import (
	"net/url"
	"time"
)

// IsURL validates if input string is a properly formatted URL
func IsURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return parsedURL.Scheme != "" && parsedURL.Host != ""
}

// IsDuration validates if input value represents a duration
// Accepts int (seconds) or string (time.Duration format like "30s", "1m")
func IsDuration(value any) bool {
	if _, ok := value.(int); ok {
		return true
	}
	if str, ok := value.(string); ok {
		_, err := time.ParseDuration(str)
		return err == nil
	}
	return false
}
