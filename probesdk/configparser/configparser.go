// Package configParser is the public mirror of the agent's duration
// parsing helper (senhub-agent.go/internal/agent/configParser).
package configParser

import (
	"time"

	iconfigParser "senhub-agent.go/internal/agent/configParser"
)

// ParseDuration parses a duration from a Go duration string or a numeric
// second count.
func ParseDuration(value any) (time.Duration, error) {
	return iconfigParser.ParseDuration(value)
}
