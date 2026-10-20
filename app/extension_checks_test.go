package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/status"
)

type nopExtension struct{}

func (nopExtension) Start(context.Context) error    { return nil }
func (nopExtension) Shutdown(context.Context) error { return nil }

func registerSample(t *testing.T, e extension.Extension) {
	t.Helper()
	extension.ResetForTest()
	t.Cleanup(extension.ResetForTest)
	e.Name, e.ConfigBlock = "sample", "sample"
	e.New = func(extension.Host) (extension.Service, error) { return nopExtension{}, nil }
	extension.Register(e)
}

func TestConfigCheckPrintsTheExtensionLines(t *testing.T) {
	var gotBlock map[string]any
	var gotPresent bool
	registerSample(t, extension.Extension{Check: func(block map[string]any, present bool) []extension.CheckLine {
		gotBlock, gotPresent = block, present
		return []extension.CheckLine{
			{Level: extension.CheckOK, Text: "sample: fine"},
			{Level: extension.CheckOff, Text: "sample: part off"},
			{Level: extension.CheckWarn, Text: "sample: careful"},
			{Level: extension.CheckError, Text: "sample.x is required"},
			{Level: "bogus", Text: "sample: odd level"},
		}
	}})

	data := configuration.LocalConfigurationData{Extensions: map[string]interface{}{"sample": map[string]interface{}{"enabled": true}}}
	var errs, warns int
	out := captureStdout(t, func() { errs, warns = reportExtensions(data) })

	if !gotPresent || gotBlock["enabled"] != true {
		t.Errorf("Check got block %v present %t", gotBlock, gotPresent)
	}
	if errs != 1 || warns != 2 {
		t.Errorf("errors=%d warnings=%d, want 1/2\n%s", errs, warns, out)
	}
	for _, want := range []string{"  [OK]   sample: fine", "  [OFF]  sample: part off", "  [WARN] sample: careful", "  [ERROR] sample.x is required", "  [WARN] sample: odd level"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	captureStdout(t, func() { reportExtensions(configuration.LocalConfigurationData{}) })
	if gotPresent {
		t.Error("an absent block must be passed as absent")
	}
}

func TestDoctorAsksTheExtensionForItsLine(t *testing.T) {
	var got extension.DoctorInput
	line := extension.DoctorLine{Level: extension.DoctorWarn, Message: "not connected", Fix: "check it"}
	registerSample(t, extension.Extension{Doctor: func(in extension.DoctorInput) extension.DoctorLine {
		got = in
		return line
	}})

	cfg := doctorConfig{data: configuration.LocalConfigurationData{Extensions: map[string]interface{}{"sample": map[string]interface{}{"enabled": true}}}}
	daemon := statusResult{source: "daemon", system: status.SystemStatus{Extensions: map[string]map[string]any{"sample": {"connected": false}}}}

	checks := extensionChecks(cfg, daemon)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v", checks)
	}
	c := checks[0]
	if c.ID != "config.sample" || c.Level != levelWarn || c.Message != "not connected" || c.Fix != "check it" || c.Section != sectionConfiguration {
		t.Errorf("check = %+v", c)
	}
	if !got.Present || got.Block["enabled"] != true || !got.AgentAnswered || got.Status["connected"] != false {
		t.Errorf("input = %+v", got)
	}

	line = extension.DoctorLine{ID: "custom.id", Level: extension.DoctorOK, Message: "fine"}
	checks = extensionChecks(cfg, statusResult{notRunning: true})
	if checks[0].ID != "custom.id" || checks[0].Level != levelOK {
		t.Errorf("check = %+v", checks[0])
	}
	if got.AgentAnswered || got.Status != nil {
		t.Errorf("agent not running, input = %+v", got)
	}

	line = extension.DoctorLine{Level: "bogus", Message: "?"}
	checks = extensionChecks(doctorConfig{err: errors.New("broken")}, daemon)
	if checks[0].Level != levelSkip {
		t.Errorf("an unknown level must become a skip, got %+v", checks[0])
	}
	if got.ConfigErr == nil || got.Present {
		t.Errorf("unloadable configuration, input = %+v", got)
	}
}
