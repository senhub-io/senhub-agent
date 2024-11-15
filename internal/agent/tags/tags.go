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

func OnlyPublicTags(tags []Tag) []Tag {
	filteredTags := make([]Tag, 0, len(tags))
	for _, tag := range tags {
		if !tag.Private {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filteredTags
}

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
