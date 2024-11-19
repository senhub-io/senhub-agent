package configParser

import (
	"fmt"
	"time"
)

func ParseDuration(value any) (time.Duration, error) {
	if _, ok := value.(float64); ok {
		// This is a duration in seconds
		return time.Duration(value.(float64)) * time.Second, nil
	}
	if _, ok := value.(string); ok {
		// This is a duration in string format
		// Check it can be parsed
		return time.ParseDuration(value.(string))
	}

	return time.Duration(0), fmt.Errorf("invalid duration format")
}
