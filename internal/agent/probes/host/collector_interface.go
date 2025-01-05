// internal/agent/probes/host/collector_interface.go
package host

import (
	"senhub-agent.go/internal/agent/services/data_store"
	"time"
)

// osCollector définit l'interface pour les collecteurs spécifiques à l'OS
type osCollector interface {
	Collect(timestamp time.Time) ([]data_store.DataPoint, error)
	Close() error
}
