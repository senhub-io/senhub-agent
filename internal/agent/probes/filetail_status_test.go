package probes_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes"
	_ "senhub-agent.go/internal/agent/probes/filetail"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
	"senhub-agent.go/internal/agent/services/statuschan"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func probeInSnapshot(t *testing.T, base *logger.Logger, name string) status.ProbeStatus {
	t.Helper()
	snap := statuschan.Snapshot(status.NewStatusService(base, "test", "test"))
	for _, p := range snap.Probes {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("probe %q is not in the status the channel serves: %+v", name, snap.Probes)
	return status.ProbeStatus{}
}

// The status channel is what `doctor` reads. A probe that delivered its
// self-metrics must show the datapoints and the time of that delivery
// there, including after its file was rotated by rename and create:
// the doctor warns "no data collected yet" on a count of zero.
func TestFiletailStatusShowsDataAfterRotation(t *testing.T) {
	base := logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"})
	dir := t.TempDir()
	file := filepath.Join(dir, "a.log")
	line := strings.Repeat("x", 100) + "\n"
	if err := os.WriteFile(file, []byte(strings.Repeat(line, 10)), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := configuration.ProbeConfig{Name: "ft-status", Type: "filetail", Params: configuration.ProbeConfigParams{
		"paths":         []interface{}{file},
		"bookmark_path": filepath.Join(dir, "bookmark.json"),
	}}
	delivered := 0
	poller, err := probes.NewProbePoller(cfg, base, func(d []datapoint.DataPoint, _ data_store.StrategyRouter) error {
		delivered += len(d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Probe.OnStart(make(chan struct{})); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = poller.Probe.OnShutdown(context.Background()) }()

	agentstate.SetActiveProbes([]string{poller.GetProbeId()})
	agentstate.SetActiveProbeNames(map[string]string{"ft-status": poller.GetProbeId()})
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
		agentstate.SetActiveProbeNames(nil)
	})

	before := probeInSnapshot(t, base, "ft-status")
	if before.MetricsCount != 0 || !before.LastUpdate.IsZero() {
		t.Fatalf("a probe that has not run a cycle reports data: %+v", before)
	}

	if err := os.Rename(file, file+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat(line, 3)), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)

	if err := poller.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := probeInSnapshot(t, base, "ft-status")
	if got.Status != "active" {
		t.Fatalf("status = %q, want active: %+v", got.Status, got)
	}
	if got.MetricsCount == 0 || got.MetricsCount != delivered {
		t.Fatalf("metrics count = %d, the cycle delivered %d", got.MetricsCount, delivered)
	}
	if time.Since(got.LastUpdate) > time.Minute {
		t.Fatalf("last update = %v, want the cycle that just ran", got.LastUpdate)
	}
}
