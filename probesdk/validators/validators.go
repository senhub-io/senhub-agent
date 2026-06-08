// Package validators is the public mirror of the agent's small input
// validators (senhub-agent.go/internal/agent/validators).
package validators

import ivalidators "senhub-agent.go/internal/agent/validators"

// IsURL reports whether urlStr parses as an absolute URL.
func IsURL(urlStr string) bool {
	return ivalidators.IsURL(urlStr)
}

// IsDuration reports whether value is a valid Go duration string or a
// numeric second count.
func IsDuration(value any) bool {
	return ivalidators.IsDuration(value)
}
