package auto_update

import (
	"testing"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
)

// settingTestConfig is a ConfigSource whose Agent section is fixed by the
// test, named uniquely so it never collides with another fake in the
// package.
type settingTestConfig struct {
	cfg configuration.ConfigurationData
}

func (c settingTestConfig) GetConfiguration() configuration.ConfigurationData {
	return c.cfg
}

func (c settingTestConfig) OnConfigChanged(func(string)) {}

func newSettingTestAutoUpdate(agent configuration.AgentConfig) *autoUpdate {
	return &autoUpdate{configSource: settingTestConfig{cfg: configuration.ConfigurationData{Agent: agent}}}
}

func TestPublishSettingLocal(t *testing.T) {
	t.Cleanup(func() {
		SetDirective(nil)
		agentstate.ResetUpdateSettingForTest()
	})
	SetDirective(nil)

	a := newSettingTestAutoUpdate(configuration.AgentConfig{UpdateEnabled: true, IncludeBeta: true})
	a.publishSetting()

	got, ok := agentstate.GetUpdateSetting()
	if !ok {
		t.Fatal("setting not published")
	}
	want := agentstate.UpdateSetting{Enabled: true, IncludeBeta: true, Target: "latest", Source: agentstate.UpdateSourceLocal, Paused: false}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPublishSettingServerDirective(t *testing.T) {
	t.Cleanup(func() {
		SetDirective(nil)
		agentstate.ResetUpdateSettingForTest()
	})
	SetDirective(&Directive{Auto: false, TargetVersion: "0.7.2"})

	a := newSettingTestAutoUpdate(configuration.AgentConfig{UpdateEnabled: true, IncludeBeta: true})
	a.publishSetting()

	got, ok := agentstate.GetUpdateSetting()
	if !ok {
		t.Fatal("setting not published")
	}
	want := agentstate.UpdateSetting{Enabled: false, IncludeBeta: true, Target: "0.7.2", Source: agentstate.UpdateSourceServer, Paused: false}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSetPausedPublishesSettingImmediately(t *testing.T) {
	t.Cleanup(func() {
		setSettingHook(nil)
		SetPaused(false)
		agentstate.ResetUpdateSettingForTest()
	})

	a := newSettingTestAutoUpdate(configuration.AgentConfig{UpdateEnabled: true})
	setSettingHook(a.publishSetting)

	SetPaused(true)
	got, ok := agentstate.GetUpdateSetting()
	if !ok {
		t.Fatal("setting not published")
	}
	if !got.Paused {
		t.Fatalf("Paused is false after SetPaused(true): %+v", got)
	}

	SetPaused(false)
	got, ok = agentstate.GetUpdateSetting()
	if !ok {
		t.Fatal("setting not published")
	}
	if got.Paused {
		t.Fatalf("Paused is true after SetPaused(false): %+v", got)
	}
}

func TestPublishSettingWithoutConfigSource(t *testing.T) {
	t.Cleanup(agentstate.ResetUpdateSettingForTest)

	a := &autoUpdate{}
	a.publishSetting()
	if _, ok := agentstate.GetUpdateSetting(); ok {
		t.Fatal("CLI updater without a config source published a setting")
	}
}
