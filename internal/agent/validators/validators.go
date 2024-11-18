package validators

import (
	"net/url"
	"time"
)

func IsURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}
	return true
}

func IsDuration(value any) bool {
	if _, ok := value.(float64); ok {
		// This is a duration in seconds
		return true
	}
	if _, ok := value.(string); ok {
		// This is a duration in string format
		// Check it can be parsed
		_, err := time.ParseDuration(value.(string))
		if err != nil {
			return false
		}
		return true
	}
	return false
}
