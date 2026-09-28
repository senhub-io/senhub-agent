package otlp

import (
	"testing"

	"senhub-agent.go/internal/agent/tags"
)

func TestFlattenTagsDropsPrivateTags(t *testing.T) {
	got := flattenTags([]tags.Tag{
		{Key: "url", Value: "https://example.com/"},
		{Key: "prtg_metric_id", Value: "https_example.com_[name]", Private: true},
	})
	if _, leaked := got["prtg_metric_id"]; leaked || got["url"] == "" {
		t.Errorf("flattenTags = %v, want url only", got)
	}
}
