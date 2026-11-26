package http

import (
	"os"
	"strings"
	"testing"
)

// Six local-console defects and their renderings.
func TestSettingsSessionPlaceholderKeyAndSaveBanner(t *testing.T) {
	html, err := NewAssetHandler("319a2cf8-7a69-4752-b183-dd4f8430c10d").RenderTemplate("settings")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `window.AGENT_KEY === '_'`) {
		t.Error("settings does not hide the administration key in a browser session")
	}
	if !strings.Contains(html, "Not shown in a browser session") {
		t.Error("settings does not explain how to get the administration key")
	}
	if !strings.Contains(html, "d.warnings && d.warnings.length ? 'warn' : 'ok'") {
		t.Error("settings keeps a warn banner after a clean save")
	}
}

func TestProbesPageKnowsWhetherPRTGIsEnabled(t *testing.T) {
	const key = "50327df5-35b0-4d3c-8288-a99ffbca544f"
	html, err := NewAssetHandlerWithPRTG(key, false).RenderTemplate("probes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "window.PRTG_ENABLED = false") {
		t.Error("probes page does not say PRTG is off")
	}
	html, err = NewAssetHandlerWithPRTG(key, true).RenderTemplate("probes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "window.PRTG_ENABLED = true") {
		t.Error("probes page does not say PRTG is on")
	}
}

func TestDashboardLinksAddAnOutputToTheAddCard(t *testing.T) {
	html, err := NewAssetHandler("50327df5-35b0-4d3c-8288-a99ffbca544f").RenderTemplate("dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(html, "/outputs#add"); n != 2 {
		t.Errorf("dashboard has %d links to the add card, want 2", n)
	}
}

func TestOutputsMenuHasNoEnableForADisabledOutput(t *testing.T) {
	html, err := NewAssetHandler("50327df5-35b0-4d3c-8288-a99ffbca544f").RenderTemplate("outputs")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, `(dis ? '<button data-act="enable">Enable</button>'`) {
		t.Error("outputs menu still shows Enable for a disabled output")
	}
}

func TestDashboardProbeOrdering(t *testing.T) {
	js, err := os.ReadFile("assets/js/dashboard.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(js)
	if !strings.Contains(s, "unsupported: 4") {
		t.Error("probe ordering leaves undefined for unsupported probes")
	}
	if strings.Contains(s, "stopped: 2") {
		t.Error("probe ordering still uses stopped, which probeState no longer returns")
	}
}
