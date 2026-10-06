package http

import "testing"

func TestAssetHandler_ParsesPagesOnFirstRender(t *testing.T) {
	ah := NewAssetHandler("key")
	if len(ah.templates) != 0 {
		t.Fatalf("handler parsed %d pages at construction, want 0", len(ah.templates))
	}

	for _, name := range consolePages {
		if _, err := ah.RenderTemplate(name); err != nil {
			t.Errorf("RenderTemplate(%s): %v", name, err)
		}
	}
	if len(ah.templates) != len(consolePages) {
		t.Errorf("parsed %d pages after rendering all, want %d", len(ah.templates), len(consolePages))
	}

	if _, err := ah.RenderTemplate("not-a-page"); err == nil {
		t.Error("rendering an unknown page succeeded")
	}
	if _, err := ah.RenderTemplate("../html/dashboard"); err == nil {
		t.Error("rendering a path outside the console pages succeeded")
	}
}
