package template

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// A value that is an identity, such as the checksum of a file, raises a
// problem when it differs from the previous one, in the syntax valid on
// Zabbix 6.0 and 7.0.
func TestAChangeAlertMetricRaisesATriggerOnChange(t *testing.T) {
	def := transformers.ProbeDefinition{ProbeName: "process", Metrics: []transformers.MetricDefinition{
		{Name: "passwd_checksum", DisplayName: "Passwd Checksum", Unit: "#", AlertOnChange: true,
			MultiInstanceLabels: []string{},
			Otel:                &transformers.OtelMapping{Name: "senhub.system.passwd.checksum", Unit: "1", Type: "gauge"}},
		{Name: "passwd_modified_time", DisplayName: "Passwd Modified Time", Unit: "s",
			MultiInstanceLabels: []string{},
			Otel:                &transformers.OtelMapping{Name: "senhub.system.passwd.modified_timestamp", Unit: "s", Type: "gauge"}},
	}}
	exp, err := Generate(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tplName := exp.ZabbixExport.Templates[0].Template
	changed := 0
	for key, p := range prototypesByKey(t, exp) {
		for _, tr := range p.TriggerPrototypes {
			changed++
			if !strings.Contains(key, "passwd.checksum") {
				t.Errorf("trigger on %s, which does not ask for one", key)
			}
			if want := "change(/" + tplName + "/" + key + ")<>0"; tr.Expression != want {
				t.Errorf("expression = %q, want %q", tr.Expression, want)
			}
		}
	}
	if changed != 1 {
		t.Errorf("change triggers = %d, want 1", changed)
	}
	if problems := Validate(exp); len(problems) > 0 {
		t.Errorf("the export would be refused: %v", problems)
	}
}

// The shipped definition of the process probe carries the trigger on the
// passwd checksum in its Linux template and nowhere else.
func TestProcessLinuxTemplateCarriesThePasswdChangeTrigger(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	def, ok := defs["process"]
	if !ok {
		t.Fatal("no process definition")
	}
	exp, err := Generate(def, Options{Platform: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for key, p := range prototypesByKey(t, exp) {
		for _, tr := range p.TriggerPrototypes {
			if strings.Contains(tr.Expression, "change(") {
				n++
				if !strings.Contains(key, "senhub.system.passwd.checksum") {
					t.Errorf("change trigger on %s", key)
				}
			}
		}
	}
	if n != 1 {
		t.Errorf("change triggers = %d, want 1", n)
	}
}
