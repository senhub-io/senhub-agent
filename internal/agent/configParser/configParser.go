// configParser/configParser.go

// Package configParser provides utilities for parsing configuration values
package configParser

import (
	"fmt"
	"time"
)

// ParseDuration converts value to time.Duration
// Accepts:
// - int: interpreted as seconds
// - string: parsed using time.ParseDuration format (e.g., "30s", "1m")
func ParseDuration(value any) (time.Duration, error) {
	if seconds, ok := value.(int); ok {
		return time.Duration(seconds) * time.Second, nil
	}
	if str, ok := value.(string); ok {
		return time.ParseDuration(str)
	}
	return time.Duration(0), fmt.Errorf("invalid duration format")
}
