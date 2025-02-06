// Package tags handles agent tags lifecycle and formatting
package tags

import (
	"fmt"
	"net/url"
)

// Tag represents a key-value metadata pair with optional privacy setting
type Tag struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Private bool   `json:"-"`
}

// OnlyPublicTags filters out private tags from the input slice
func OnlyPublicTags(tags []Tag) []Tag {
	filteredTags := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		if !tag.Private {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filteredTags
}

// UrlToTagKey converts URL string to a tag key format
// Example: "http://example.com:8080" -> "http_example.com_8080"
func UrlToTagKey(urlString string) (string, error) {
	url, err := url.Parse(urlString)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	tagKey := fmt.Sprintf("%s_%s", url.Scheme, url.Hostname())
	if port := url.Port(); port != "" {
		tagKey = fmt.Sprintf("%s_%s", tagKey, port)
	}
	return tagKey, nil
}

// FormatTagsForServer formats tags for server sending
func FormatTagsForServer(tags []Tag) []Tag {
	formattedTags := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		formattedTags = append(formattedTags, Tag{
			Key:   EscapeTagKey(tag.Key),
			Value: EscapeTagValue(tag.Value),
		})
	}
	return formattedTags
}

// EscapeTagKey escapes special characters in tag key
func EscapeTagKey(key string) string {
	return key
}

// EscapeTagValue escapes special characters in tag value
func EscapeTagValue(value string) string {
	return value
}

// TagToString returns string representation of a tag
func TagToString(tag Tag) string {
	return fmt.Sprintf("%s:%s", tag.Key, tag.Value)
}
