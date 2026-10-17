package process

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

func pointNames(pts []data_store.DataPoint) map[string]bool {
	out := map[string]bool{}
	for _, p := range pts {
		out[p.Name] = true
	}
	return out
}

func TestAppendProcessPointsSkipsUnreadValues(t *testing.T) {
	ts := time.Now()
	snap := processSnapshot{
		pid: 42, name: "svc", fds: -1,
		cpuUnread: true, memoryUnread: true, threadsUnread: true,
	}

	got := pointNames(appendProcessPoints(nil, ts, snap, nil))

	for _, name := range []string{
		"process.cpu.utilization",
		"process.memory.usage",
		"process.memory.virtual_memory_usage",
		"process.threads",
	} {
		if got[name] {
			t.Errorf("%s published although the read failed", name)
		}
	}
}

func TestAppendProcessPointsKeepsGenuineZero(t *testing.T) {
	snap := processSnapshot{pid: 42, name: "idle", fds: -1}

	got := pointNames(appendProcessPoints(nil, time.Now(), snap, nil))

	for _, name := range []string{
		"process.cpu.utilization",
		"process.memory.usage",
		"process.memory.virtual_memory_usage",
		"process.threads",
	} {
		if !got[name] {
			t.Errorf("%s missing for a process that read as zero", name)
		}
	}
}
