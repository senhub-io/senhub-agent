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
// Accepts float64 (seconds) or string (time.Duration format)
func IsDuration(value any) bool {
	if _, ok := value.(float64); ok {
		return true
	}
	if str, ok := value.(string); ok {
		_, err := time.ParseDuration(str)
		return err == nil
	}
	return false
}
