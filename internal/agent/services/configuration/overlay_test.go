package configuration

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/logger"
)

func writeOverlayTree(t *testing.T, root string) string {
	t.Helper()
	writeTestFile(t, root, "agent.yaml", "config_version: 3\nagent:\n  key: overlay-agent-key-0123\n")
	writeTestFile(t, root, "probes.d/01-host.yaml", `- name: cpu
  type: cpu
  params:
    interval: 300
- name: memory
  type: memory
  params:
    interval: 30
- name: off-locally
  type: cpu
  enabled: false
  params:
    interval: 30
`)
	writeTestFile(t, root, "strategies.d/00-http.yaml", "http:\n  type: http\n  params:\n    port: 8080\n")
	return filepath.Join(root, "agent.yaml")
}

func overlayConfiguration(t *testing.T, path string) *LocalConfiguration {
	t.Helper()
	zl := zerolog.Nop()
	base := (*logger.Logger)(&zl)
	return &LocalConfiguration{
		logger:        logger.NewModuleLogger(base, "configuration.local.test"),
		configPath:    path,
		eventNotifier: NewEventNotifier(base),
	}
}

func registerOverlay(t *testing.T, fn func(extension.OverlayTarget)) {
	t.Helper()
	extension.ResetForTest()
	t.Cleanup(extension.ResetForTest)
	extension.Register(extension.Extension{
		Name:    "overlay",
		New:     func(extension.Host) (extension.Service, error) { return nopService{}, nil },
		Overlay: fn,
	})
}

func probeByName(t *testing.T, d *LocalConfigurationData, name string) ProbeConfig {
	t.Helper()
	for _, p := range d.Probes {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("probe %s not loaded", name)
	return ProbeConfig{}
}

func TestOverlayChangesTheLoadedConfigurationNotTheFiles(t *testing.T) {
	root := t.TempDir()
	path := writeOverlayTree(t, root)
	before, err := os.ReadFile(filepath.Join(root, "probes.d/01-host.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var seen []extension.OverlayProbe
	registerOverlay(t, func(o extension.OverlayTarget) {
		seen = o.Probes()
		if !o.SetProbeInterval("cpu", 60) || !o.SetProbeEnabled("memory", false) {
			t.Error("a probe of the local configuration must be found")
		}
		if o.SetProbeEnabled("absent", true) {
			t.Error("a probe the configuration does not have must be reported missing")
		}
		o.SetEntities(extension.EntitySwitches{Enabled: true, DependsOn: true})
	})

	lc := overlayConfiguration(t, path)
	if err := lc.loadConfiguration(); err != nil {
		t.Fatal(err)
	}
	d := lc.snapshot()

	if got, _ := wholeSeconds(probeByName(t, d, "cpu").Params["interval"]); got != 60 {
		t.Errorf("cpu interval = %d, want 60", got)
	}
	if probeByName(t, d, "memory").IsEnabled() {
		t.Error("memory must be disabled by the overlay")
	}
	if d.Entities == nil || !d.Entities.Enabled || d.Entities.DependsOn == nil || !d.Entities.DependsOn.Enabled {
		t.Errorf("entities = %+v, want enabled with depends_on", d.Entities)
	}
	if len(seen) != 3 || seen[0].Interval != 300 || !seen[2].DisabledLocally || seen[0].DisabledLocally {
		t.Errorf("overlay saw %+v", seen)
	}

	after, err := os.ReadFile(filepath.Join(root, "probes.d/01-host.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("an overlay must never rewrite a configuration file")
	}
}

func TestReloadAppliesAChangedOverlayAndNotifies(t *testing.T) {
	path := writeOverlayTree(t, t.TempDir())
	var interval atomic.Int64
	interval.Store(300)
	registerOverlay(t, func(o extension.OverlayTarget) { o.SetProbeInterval("cpu", int(interval.Load())) })

	lc := overlayConfiguration(t, path)
	if err := lc.loadConfiguration(); err != nil {
		t.Fatal(err)
	}
	notified := make(chan struct{}, 4)
	lc.OnConfigChanged(func(string) { notified <- struct{}{} })

	if err := lc.Reload(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notified:
		t.Fatal("a reload that changes nothing must not notify")
	case <-time.After(50 * time.Millisecond):
	}

	interval.Store(60)
	if err := lc.Reload(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("a reload whose overlay changed a probe must notify")
	}
	if got, _ := wholeSeconds(probeByName(t, lc.snapshot(), "cpu").Params["interval"]); got != 60 {
		t.Errorf("cpu interval = %d, want 60", got)
	}
}

func TestOverlayDoesNotShareParamsWithThePreviousConfiguration(t *testing.T) {
	cfg := LocalConfigurationData{Probes: []ProbeConfig{{Name: "cpu", Params: ProbeConfigParams{"interval": 300}}}}
	shared := cfg.Probes[0].Params
	(&overlayTarget{cfg: &cfg}).SetProbeInterval("cpu", 60)
	if shared["interval"] != 300 {
		t.Error("the overlay wrote into a params map another configuration may hold")
	}
}
