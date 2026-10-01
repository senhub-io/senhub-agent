//go:build !linux && !windows

package logicaldisk

import (
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func (c *unixLogicalDiskCollector) collectDiskIO(time.Time, []tags.Tag) ([]data_store.DataPoint, error) {
	return nil, nil
}
