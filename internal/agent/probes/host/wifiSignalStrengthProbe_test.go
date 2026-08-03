package host

import (
	"os"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/data_store"
)

func TestNewWifiSignalStrengthProbe(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	tests := []struct {
		name    string
		config  map[string]interface{}
		wantErr bool
	}{
		{
			name:    "Valid Probe",
			config:  map[string]interface{}{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewWifiSignalStrengthProbe(tt.config, &logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewWifiSignalStrengthProbe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

// TestWifiProbe_DatapointsCarryProbeTags pins #264: wifi datapoints
// must carry probe_name/probe_type like every other host probe — the
// enrichment used to sit in a dead conditional branch (a never-wired
// callback, removed in #166), so the transformer, per-probe
// custom_tags and OTLP partitioning all missed wifi series.
func TestWifiProbe_DatapointsCarryProbeTags(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	probe, err := NewWifiSignalStrengthProbe(map[string]interface{}{"interval": 30}, &logger)
	if err != nil {
		t.Fatalf("NewWifiSignalStrengthProbe: %v", err)
	}
	wifi, ok := probe.(*wifiSignalStrengthProbe)
	if !ok {
		t.Fatal("unexpected probe type")
	}
	// Mirror the ProbePoller wiring (probe_poller.go): name and type
	// come from the probe configuration at startup.
	wifi.SetName("wifi-office")
	wifi.SetProbeType("wifi_signal_strength")

	points := wifi.finish([]data_store.DataPoint{{Name: "wifi_signal_strength", Value: -42}})
	if len(points) != 1 {
		t.Fatalf("expected 1 datapoint, got %d", len(points))
	}
	tagsByKey := map[string]string{}
	for _, tg := range points[0].Tags {
		tagsByKey[tg.Key] = tg.Value
	}
	if tagsByKey["probe_name"] != "wifi-office" {
		t.Errorf("probe_name = %q, want wifi-office (#264)", tagsByKey["probe_name"])
	}
	if tagsByKey["probe_type"] != "wifi_signal_strength" {
		t.Errorf("probe_type = %q, want wifi_signal_strength", tagsByKey["probe_type"])
	}
}
