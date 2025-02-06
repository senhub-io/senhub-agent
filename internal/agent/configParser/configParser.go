// configParser/configParser.go

// Package configParser provides utilities for parsing configuration values
package configParser

import (
	"fmt"
	"time"
)

// ParseDuration converts value to time.Duration
// Accepts:
// - float64: interpreted as seconds
// - string: parsed using time.ParseDuration format
func ParseDuration(value any) (time.Duration, error) {
	if seconds, ok := value.(float64); ok {
		return time.Duration(seconds) * time.Second, nil
	}
	if str, ok := value.(string); ok {
		return time.ParseDuration(str)
	}
	return time.Duration(0), fmt.Errorf("invalid duration format")
}
