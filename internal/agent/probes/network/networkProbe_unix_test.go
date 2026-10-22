//go:build !windows

package network

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestUnixNetworkCollector_FirstCollectPrimes(t *testing.T) {
	baseLogger := logger.NewLogger(&cliArgs.ParsedArgs{})
	collector, err := newNetworkCollector(map[string]interface{}{}, baseLogger)
	if err != nil {
		t.Fatalf("newNetworkCollector() error = %v", err)
	}

	u := collector.(*unixNetworkCollector)
	u.primeWait = 10 * time.Millisecond

	now := time.Now()
	dataPoints, err := u.Collect(now)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	monitoredFound := false
	for _, dp := range dataPoints {
		if dp.Name != "bytes_received" {
			continue
		}
		monitoredFound = true
		if dp.Value < 0 {
			t.Errorf("bytes_received = %v, want >= 0", dp.Value)
		}
	}

	for name, last := range u.lastCounters {
		if !last.Timestamp.After(now) {
			t.Errorf("%s: the primed reading must be dated after the collection time, got %v for %v", name, last.Timestamp, now)
		}
	}

	if !monitoredFound {
		t.Skip("no monitored interface emitted bytes_received; loopback is skipped and no other interface is available")
	}
}
