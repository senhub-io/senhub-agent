package tags

import (
	"fmt"
	"net/url"
)

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// Ability to mark a tag as private, which means it should not be sent to the server.
	Private bool `json:"-"`
}

// Filter out private tags.
func OnlyPublicTags(tags []Tag) []Tag {
	filteredTags := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		if !tag.Private {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filteredTags
}

// Convert a URL to a tag key.
// Examples:
// http://example.com:8080 -> http_example.com_8080
// https://example.com -> https_example.com
func UrlToTagKey(urlString string) (string, error) {
	url, err := url.Parse(urlString)
	if err != nil {
		return "", err
	}

	tagKey := fmt.Sprintf("%s_%s", url.Scheme, url.Hostname())
	port := url.Port()
	if port != "" {
		tagKey = fmt.Sprintf("%s_%s", tagKey, port)
	}

	return tagKey, nil

}

func FormatTagsForServer(tags []Tag) []Tag {
	formattedTags := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		formattedTags = append(formattedTags, Tag{Key: EscapeTagKey(tag.Key), Value: EscapeTagValue(tag.Value)})
	}
	return formattedTags
}

func EscapeTagKey(key string) string {
	return key
}

func EscapeTagValue(value string) string {
	return value
}

func TagToString(tag Tag) string {
	return fmt.Sprintf("%s:%s", tag.Key, tag.Value)
}
