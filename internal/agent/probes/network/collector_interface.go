// internal/agent/probes/host/collector_interface.go
package network

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
)

// osCollector defines the interface for OS-specific collectors
type osCollector interface {
	Collect(timestamp time.Time) ([]data_store.DataPoint, error)
	Close() error
}
