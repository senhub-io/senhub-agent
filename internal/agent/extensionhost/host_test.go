package extensionhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/exporthook"
	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

func nopLogger() *logger.Logger {
	zl := zerolog.Nop()
	return (*logger.Logger)(&zl)
}

type fakeConfig struct {
	blocks    map[string]map[string]interface{}
	callbacks []func(string)
	key       string
	path      string
	data      configuration.ConfigurationData
}

func (f *fakeConfig) ExtensionBlock(name string) (map[string]interface{}, bool) {
	b, ok := f.blocks[name]
	return b, ok
}
func (f *fakeConfig) OnConfigChanged(cb func(string)) { f.callbacks = append(f.callbacks, cb) }
func (f *fakeConfig) GetAuthenticationKey() string    { return f.key }
func (f *fakeConfig) GetConfigPath() string           { return f.path }
func (f *fakeConfig) GetConfiguration() configuration.ConfigurationData {
	return f.data
}

type recordingService struct {
	started, stopped bool
	host             extension.Host
}

func (r *recordingService) Start(context.Context) error    { r.started = true; return nil }
func (r *recordingService) Shutdown(context.Context) error { r.stopped = true; return nil }

func TestServicesBuildsEachValidExtension(t *testing.T) {
	extension.ResetForTest()
	t.Cleanup(extension.ResetForTest)

	rec := &recordingService{}
	extension.Register(extension.Extension{Name: "good", StopBudget: 7 * time.Second,
		New: func(h extension.Host) (extension.Service, error) { rec.host = h; return rec, nil }})
	extension.Register(extension.Extension{Name: "broken",
		New: func(extension.Host) (extension.Service, error) { return nil, errors.New("boom") }})
	extension.Register(extension.Extension{Name: "empty",
		New: func(extension.Host) (extension.Service, error) { return nil, nil }})
	extension.Register(extension.Extension{Name: "good", New: func(extension.Host) (extension.Service, error) { return rec, nil }})

	svcs := Services(Deps{Logger: nopLogger()})
	if len(svcs) != 1 {
		t.Fatalf("Services built %d services, want only the valid one", len(svcs))
	}
	svc := svcs[0]
	if svc.GetName() != "Extension good" {
		t.Errorf("GetName = %q", svc.GetName())
	}
	if got := lifecycle.TotalStopBudget(svc); got != 7*time.Second {
		t.Errorf("stop budget = %v, want the declared 7s", got)
	}

	sup := lifecycle.NewSupervisor(nopLogger())
	if errs := sup.Start(context.Background(), svcs...); len(errs) > 0 {
		t.Fatal(errs)
	}
	rec.host.PublishStatus(map[string]any{"connected": true})
	if extension.Statuses()["good"]["connected"] != true {
		t.Error("PublishStatus did not reach the status registry")
	}
	if errs := sup.Shutdown(context.Background()); len(errs) > 0 {
		t.Fatal(errs)
	}
	if !rec.started || !rec.stopped {
		t.Errorf("started=%t stopped=%t, want the supervisor to run both", rec.started, rec.stopped)
	}
	if extension.Statuses() != nil {
		t.Error("a stopped extension must not keep reporting a status")
	}
}

func TestHostReadsTheAgent(t *testing.T) {
	cfg := &fakeConfig{
		blocks: map[string]map[string]interface{}{"sample": {"enabled": true}},
		key:    "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
	}
	h := &host{deps: Deps{Logger: nopLogger(), Config: cfg, Version: "1.2.3",
		IsProbeRegistered: func(string) bool { return false }}}

	if b, ok := h.ConfigBlock("sample"); !ok || b["enabled"] != true {
		t.Errorf("ConfigBlock = %v, %t", b, ok)
	}
	if _, ok := h.ConfigBlock("other"); ok {
		t.Error("an absent block must be reported absent")
	}

	calls := 0
	h.OnConfigChange(func() { calls++ })
	for _, cb := range cfg.callbacks {
		cb("changed")
	}
	if calls != 1 {
		t.Errorf("OnConfigChange callback ran %d times, want 1", calls)
	}

	id := h.Identity()
	want := extension.Identity{
		InstanceID:  configuration.AgentInstanceID(cfg.key),
		Version:     "1.2.3",
		Edition:     "oss",
		LicenseTier: "free",
	}
	if id != want {
		t.Errorf("Identity = %+v, want %+v", id, want)
	}

	got, gotErr := h.HostAttributes()
	ref, refErr := common.GetHostResourceAttributes()
	if (gotErr == nil) != (refErr == nil) || got["host.id"] != ref["host.id"] {
		t.Errorf("HostAttributes = %v (%v), want the OTLP resource source %v (%v)", got, gotErr, ref, refErr)
	}

	exporthook.SetAuthorizer(func(f string) bool { return f == "feature_x" })
	t.Cleanup(func() { exporthook.SetAuthorizer(nil) })
	if !h.Authorized("feature_x") || h.Authorized("feature_y") {
		t.Error("Authorized must ask the licence authorizer")
	}
}

func TestHostEffectiveConfigIsTheRedactedFileView(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte("config_version: 3\nagent:\n  key: inline-agent-key-0123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &host{deps: Deps{Logger: nopLogger(), Config: &fakeConfig{path: path}},
		log: logger.NewModuleLogger(nopLogger(), "extension")}

	files, err := h.EffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}
	want, err := configuration.RedactedConfigFiles(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want) || files[0].Name != "agent.yaml" || string(files[0].Body) != string(want[0].Body) {
		t.Errorf("EffectiveConfig = %+v, want %+v", files, want)
	}
}

func TestCollectHealth(t *testing.T) {
	agentstate.ResetStrategyFailuresForTest()
	agentstate.ResetExportActivityForTest()
	t.Cleanup(func() {
		agentstate.SetActiveProbes(nil)
		agentstate.SetStartFailedProbes(nil)
		agentstate.ResetStrategyFailuresForTest()
		agentstate.ResetExportActivityForTest()
	})

	off := false
	ok := configuration.ProbeConfig{Name: "ok", Type: "cpu"}
	bad := configuration.ProbeConfig{Name: "bad", Type: "cpu", Params: map[string]interface{}{"x": 1}}
	disabled := configuration.ProbeConfig{Name: "disabled", Type: "cpu", Enabled: &off}
	agentstate.SetActiveProbes([]string{ok.ID()})
	agentstate.RecordProbeHealth(ok.ID(), true)
	agentstate.SetStartFailedProbes(map[string]string{bad.ID(): "no route"})
	agentstate.RecordStrategyFailure("dead", "config", "missing endpoint")

	start := time.Now().Add(-time.Minute)
	cfg := configuration.ConfigurationData{
		Probes:        []configuration.ProbeConfig{ok, bad, disabled},
		StorageConfig: []configuration.StorageConfig{{Name: "dead"}, {Name: "live"}},
	}
	h := CollectHealth(cfg, start)

	if h.Healthy {
		t.Error("a failed probe and a dead output must make the agent unhealthy")
	}
	if !h.StartTime.Equal(start) {
		t.Errorf("StartTime = %v, want %v", h.StartTime, start)
	}
	if len(h.Probes) != 3 || len(h.Outputs) != 2 {
		t.Fatalf("health = %+v", h)
	}
	if p := h.Probes[0]; !p.Running || p.Health != "ok" || p.StartedAt.IsZero() {
		t.Errorf("running probe = %+v", p)
	}
	if p := h.Probes[1]; p.Running || p.Health != "failed" || p.LastError != "could not start: no route" {
		t.Errorf("failed probe = %+v", p)
	}
	if p := h.Probes[2]; p.Enabled {
		t.Errorf("disabled probe = %+v", p)
	}
	if o := h.Outputs[0]; o.FailureReason != "config" || o.FailureDetail != "missing endpoint" {
		t.Errorf("dead output = %+v", o)
	}
	if o := h.Outputs[1]; o.FailureReason != "" || o.Failing {
		t.Errorf("live output = %+v", o)
	}

	agentstate.ResetStrategyFailuresForTest()
	agentstate.SetStartFailedProbes(nil)
	if h := CollectHealth(cfg, start); !h.Healthy {
		t.Errorf("with every enabled probe and output fine, Healthy = false: %+v", h)
	}
}

func TestEdition(t *testing.T) {
	if len(license.KnownPaidProbes()) == 0 {
		t.Skip("no paid probes in the catalogue")
	}
	if got := Edition(func(string) bool { return true }); got != "enterprise" {
		t.Errorf("Edition(all) = %q, want enterprise", got)
	}
	if got := Edition(func(string) bool { return false }); got != "oss" {
		t.Errorf("Edition(none) = %q, want oss", got)
	}
	if got := Edition(nil); got != "" {
		t.Errorf("Edition(nil) = %q, want empty", got)
	}
}

type fakeValidator struct {
	lic     *license.License
	err     error
	inGrace bool
}

func (f fakeValidator) ValidateLicense(string) (*license.License, error) { return f.lic, f.err }
func (f fakeValidator) IsProbeAuthorized(*license.License, string) bool  { return true }
func (f fakeValidator) IsInGracePeriod(*license.License) bool            { return f.inGrace }

func TestLicenseTier(t *testing.T) {
	v := func(f fakeValidator) func() (license.Validator, error) {
		return func() (license.Validator, error) { return f, nil }
	}
	cases := []struct {
		name  string
		token string
		newV  func() (license.Validator, error)
		want  string
	}{
		{"no token", "", nil, "free"},
		{"no validator", "tok", func() (license.Validator, error) { return nil, errors.New("x") }, ""},
		{"invalid", "tok", v(fakeValidator{err: errors.New("bad")}), "free"},
		{"pro", "tok", v(fakeValidator{lic: &license.License{Tier: license.TierPro}}), "pro"},
		{"expired past grace", "tok", v(fakeValidator{lic: &license.License{Tier: license.TierEnterprise, IsExpired: true}}), "free"},
		{"expired in grace", "tok", v(fakeValidator{lic: &license.License{Tier: license.TierEnterprise, IsExpired: true}, inGrace: true}), "enterprise"},
	}
	for _, c := range cases {
		if got := LicenseTier(c.token, "agent-key", c.newV); got != c.want {
			t.Errorf("%s: tier = %q, want %q", c.name, got, c.want)
		}
	}
}
