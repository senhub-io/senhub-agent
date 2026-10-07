package configuration

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func snmpProbe(name string, params map[string]interface{}) ProbeConfig {
	return ProbeConfig{Name: name, Type: "snmp_poll", Params: params}
}

func TestSNMPPollTargetOverlaps(t *testing.T) {
	off := false
	disabled := snmpProbe("off", map[string]interface{}{"target": "10.0.0.1"})
	disabled.Enabled = &off

	list := []ProbeConfig{
		snmpProbe("core-a", map[string]interface{}{"target": "10.0.0.1"}),
		snmpProbe("core-b", map[string]interface{}{"target": " 10.0.0.1 ", "port": 161, "community": "public"}),
		snmpProbe("other-community", map[string]interface{}{"target": "10.0.0.1", "community": "ro-view"}),
		snmpProbe("other-port", map[string]interface{}{"target": "10.0.0.1", "port": 1161}),
		disabled,
		snmpProbe("sw-a", map[string]interface{}{"target": "SW1.Example.", "version": "v3", "v3": map[string]interface{}{"username": "mon"}}),
		snmpProbe("sw-b", map[string]interface{}{"target": "sw1.example", "port": float64(161), "version": "v3", "v3": map[string]interface{}{"username": "mon"}}),
		snmpProbe("sw-c", map[string]interface{}{"target": "sw1.example", "version": "v3", "v3": map[string]interface{}{"username": "other"}}),
		{Name: "elsewhere", Type: "ping", Params: map[string]interface{}{"target": "10.0.0.1"}},
		snmpProbe("no-target", map[string]interface{}{}),
	}
	want := []SNMPTargetOverlap{
		{Target: "10.0.0.1:161", Probes: []string{"core-a", "core-b"}},
		{Target: "sw1.example:161", Probes: []string{"sw-a", "sw-b"}},
	}
	if got := SNMPPollTargetOverlaps(list); !reflect.DeepEqual(got, want) {
		t.Errorf("SNMPPollTargetOverlaps = %+v, want %+v", got, want)
	}
}

func TestSNMPPollTargetOverlaps_NoneWhenDistinct(t *testing.T) {
	list := []ProbeConfig{
		snmpProbe("a", map[string]interface{}{"target": "10.0.0.1"}),
		snmpProbe("b", map[string]interface{}{"target": "10.0.0.2"}),
	}
	if got := SNMPPollTargetOverlaps(list); len(got) != 0 {
		t.Errorf("distinct targets reported as overlapping: %+v", got)
	}
}

func TestWarnSNMPPollOverlaps_OneWarnNamingBothProbesNoCredential(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	log := logger.NewModuleLogger((*logger.Logger)(&zl), "configuration")

	warnSNMPPollOverlaps(log, []ProbeConfig{
		snmpProbe("core-a", map[string]interface{}{"target": "10.0.0.1", "community": "s3cret-community"}),
		snmpProbe("core-b", map[string]interface{}{"target": "10.0.0.1", "community": "s3cret-community"}),
	})

	out := buf.String()
	if n := bytes.Count(buf.Bytes(), []byte("\n")); n != 1 {
		t.Fatalf("want exactly one log line, got %d: %s", n, out)
	}
	for _, need := range []string{`"level":"warn"`, "core-a", "core-b", "10.0.0.1:161"} {
		if !bytes.Contains(buf.Bytes(), []byte(need)) {
			t.Errorf("log line lacks %q: %s", need, out)
		}
	}
	if bytes.Contains(buf.Bytes(), []byte("s3cret-community")) {
		t.Errorf("log line leaks the community: %s", out)
	}
}
