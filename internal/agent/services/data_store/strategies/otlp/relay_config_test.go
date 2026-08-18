package otlp

import "testing"

// The switch was introduced as signals.traces.relay_enrichment, when only
// traces were relayed. Logs and metrics joined the same contract later, so a key
// naming one signal silently decided three (#766). The relay-level key names
// what it governs; the old one still has to work, because a host that turned it
// off there meant "do not enrich what I relay".
func TestRelayEnrichmentPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		relay      RelayConfig
		traces     TracesSignal
		wantEnable bool
	}{
		{
			name:       "neither key set: on, the historical default",
			relay:      RelayConfig{Enrichment: true},
			traces:     TracesSignal{RelayEnrichment: true},
			wantEnable: true,
		},
		{
			name:       "deprecated key off, relay key unset: still honoured",
			relay:      RelayConfig{Enrichment: true},
			traces:     TracesSignal{RelayEnrichment: false},
			wantEnable: false,
		},
		{
			name:       "relay key off wins over the deprecated key",
			relay:      RelayConfig{Enrichment: false, enrichmentSet: true},
			traces:     TracesSignal{RelayEnrichment: true},
			wantEnable: false,
		},
		{
			// The case that matters most: an operator who turned it off under
			// traces years ago and now turns it ON at the relay level means the
			// new one. Falling back would ignore an explicit instruction.
			name:       "relay key on wins over a deprecated off",
			relay:      RelayConfig{Enrichment: true, enrichmentSet: true},
			traces:     TracesSignal{RelayEnrichment: false},
			wantEnable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRelayEnrichment(tc.relay, tc.traces); got != tc.wantEnable {
				t.Errorf("enrichment = %v, want %v", got, tc.wantEnable)
			}
		})
	}
}

func TestParseRelay(t *testing.T) {
	t.Run("absent block defaults to on and unset", func(t *testing.T) {
		var r RelayConfig
		if err := parseRelay(nil, &r); err != nil {
			t.Fatalf("parseRelay: %v", err)
		}
		if !r.Enrichment {
			t.Error("default must be on")
		}
		if r.enrichmentSet {
			t.Error("an absent block must not count as the operator stating a value — " +
				"otherwise it would override the deprecated key that is still in use")
		}
	})

	t.Run("explicit false is recorded as set", func(t *testing.T) {
		var r RelayConfig
		if err := parseRelay(map[string]interface{}{"enrichment": false}, &r); err != nil {
			t.Fatalf("parseRelay: %v", err)
		}
		if r.Enrichment || !r.enrichmentSet {
			t.Errorf("enrichment=%v set=%v, want false/true", r.Enrichment, r.enrichmentSet)
		}
	})

	t.Run("a non-boolean is refused", func(t *testing.T) {
		var r RelayConfig
		if err := parseRelay(map[string]interface{}{"enrichment": "yes"}, &r); err == nil {
			t.Error(`enrichment: "yes" must be a configuration error, not a silent default`)
		}
	})

	t.Run("a non-map block is refused", func(t *testing.T) {
		var r RelayConfig
		if err := parseRelay("enabled", &r); err == nil {
			t.Error("a scalar relay block must be a configuration error")
		}
	})
}
