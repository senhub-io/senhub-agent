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
// It carries the agent's own three items and the machine's nameplate,
// which Zabbix files in host inventory.
func TestTheBaseTemplateCarriesTheAgentsOwnItemsAndTheNameplate(t *testing.T) {
	exp := template.Base(template.Options{Prefix: "senhub"})
	tpl := exp.ZabbixExport.Templates[0]
	if tpl.Template != template.BaseName {
		t.Fatalf("template = %s", tpl.Template)
	}
	if len(tpl.DiscoveryRules) != 0 {
		t.Errorf("the base template discovers nothing; rules = %+v", tpl.DiscoveryRules)
	}
	declared := map[string]template.Item{}
	for _, i := range tpl.Items {
		declared[i.Key] = i
		if i.Type != "ZABBIX_ACTIVE" || i.UUID == "" {
			t.Errorf("item %+v", i)
		}
	}
	for _, it := range agentItems("web-01") {
		if _, ok := declared[it.Key]; !ok {
			t.Errorf("the agent sends %s but the base template does not declare it", it.Key)
		}
	}
	for _, f := range template.NameplateFields {
		key := "senhub." + f.Key
		got, ok := declared[key]
		if !ok {
			t.Errorf("the nameplate field %s has no item", f.Attribute)
			continue
		}
		// Without the link the value arrives and fills nothing, which
		// is the whole point of sending it.
		if got.InventoryLink != f.Inventory {
			t.Errorf("%s fills %q, want the inventory field %q", key, got.InventoryLink, f.Inventory)
		}
		if got.ValueType != "CHAR" {
			t.Errorf("%s has value type %q; an operating system name is text", key, got.ValueType)
		}
	}
	if want := len(agentItems("web-01")) + len(template.NameplateFields); len(declared) != want {
		t.Errorf("the template declares %d items, want %d", len(declared), want)
	}
}

// A machine the agent learned nothing about must not blank the
// inventory it already has: an empty value would overwrite a field an
// operator filled by hand.
func TestAnUnknownNameplateSendsNothingRatherThanEmptyValues(t *testing.T) {
	s := &Strategy{}
	s.cfg.KeyPrefix = "senhub"
	if got := s.nameplateItems(); len(got) != 0 {
		t.Fatalf("items = %+v, want none before anything is known", got)
	}
	s.setNameplate(map[string]any{"os.name": "  Ubuntu  ", "hw.vendor": "   ", "host.arch": "arm64"})
	got := s.nameplateItems()
	if len(got) != 1 {
		t.Fatalf("items = %+v, want the one fact that has a value and a field", got)
	}
	if got[0].Key != "senhub.host.os" || got[0].Value != "Ubuntu" {
		t.Fatalf("item = %+v; the value is trimmed and keyed on its field", got[0])
	}
}
