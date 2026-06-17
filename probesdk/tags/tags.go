// Package tags is the public mirror of the agent's metric tag type
// (senhub-agent.go/internal/agent/tags).
package tags

import itags "senhub-agent.go/internal/agent/tags"

// Tag is a single metric tag (key/value with category semantics).
type Tag = itags.Tag

// UrlToTagKey derives a stable tag key from a URL string.
func UrlToTagKey(urlString string) (string, error) {
	return itags.UrlToTagKey(urlString)
}
