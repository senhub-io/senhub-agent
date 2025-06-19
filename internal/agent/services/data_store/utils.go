// senhub-agent/internal/agent/services/data_store/utils.go

package data_store

import (
	"senhub-agent.go/internal/agent/tags"
)

// PRTG utility functions

var (
	// Value of the tag with this name will be used as PRTG metric id.
	// The placeholder [name] will be replaced by the metric name.
	PrtgTagName = "prtg_metric_id"
)

// CreatePrtgMetricIdTag creates a PRTG metric ID tag for probes to use
func CreatePrtgMetricIdTag(metricId string) tags.Tag {
	return tags.Tag{
		Key:     PrtgTagName,
		Value:   metricId,
		Private: true,
	}
}
