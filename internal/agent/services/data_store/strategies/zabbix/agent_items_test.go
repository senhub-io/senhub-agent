package zabbix

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
)

func TestTheAgentsOwnItemsAreServedWhateverIsCollected(t *testing.T) {
	its := agentItems("web-01")
	got := map[string]string{}
	for _, it := range its {
		got[it.Key] = it.Value
	}
	if got["agent.ping"] != "1" {
		t.Errorf("agent.ping = %q; the host's availability hangs on it", got["agent.ping"])
	}
	if got["agent.hostname"] != "web-01" {
		t.Errorf("agent.hostname = %q", got["agent.hostname"])
	}
	if got["agent.version"] == "" {
		t.Error("agent.version is empty; a build without its version stamp must still answer something")
	}
}

func TestThePassiveListenerAnswersFromTheSameSource(t *testing.T) {
	p := &passiveListener{hostname: "web-01"}
	for _, it := range agentItems("web-01") {
		v, ok := p.resolve(it.Key)
		if !ok || v != it.Value {
			t.Errorf("%s: passive says %q/%v, the active push sends %q", it.Key, v, ok, it.Value)
		}
	}
}

// The base template is what makes those items exist on the server. It
// must stand alone, because Zabbix refuses two linked templates that
// declare one key and every probe template is linked beside the others.
func TestTheBaseTemplateCarriesTheAgentsOwnItemsAndNothingElse(t *testing.T) {
	exp := template.Base(template.Options{Prefix: "senhub"})
	tpl := exp.ZabbixExport.Templates[0]
	if tpl.Template != template.BaseName {
		t.Fatalf("template = %s", tpl.Template)
	}
	if len(tpl.DiscoveryRules) != 0 {
		t.Errorf("the base template discovers nothing; rules = %+v", tpl.DiscoveryRules)
	}
	declared := map[string]bool{}
	for _, i := range tpl.Items {
		declared[i.Key] = true
		if i.Type != "ZABBIX_ACTIVE" || i.UUID == "" {
			t.Errorf("item %+v", i)
		}
	}
	for _, it := range agentItems("web-01") {
		if !declared[it.Key] {
			t.Errorf("the agent sends %s but the base template does not declare it", it.Key)
		}
	}
	if len(declared) != len(agentItems("web-01")) {
		t.Errorf("the template declares %d items, the agent sends %d", len(declared), len(agentItems("web-01")))
	}
}
