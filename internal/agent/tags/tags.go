package tags

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
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

// TODO: In a near future tags will be sent as a list of strings, not a map.
func FormatTagsForServer(tags []Tag) map[string]string {
	formattedTags := make(map[string]string)
	for _, tag := range tags {
		formattedTags[tag.Key] = tag.Value
	}
	return formattedTags
}

func EscapeTagKey(key string) string {
	re := regexp.MustCompile("[:]")
	return strings.ReplaceAll(re.ReplaceAllString(key, "_"), ".", "_")
}

func EscapeTagValue(value string) string {
	re := regexp.MustCompile("[:]")
	return strings.ReplaceAll(re.ReplaceAllString(value, "_"), ".", "_")
}

func TagToString(tag Tag) string {
	return fmt.Sprintf("%s:%s", tag.Key, tag.Value)
}
