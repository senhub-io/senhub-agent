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

// iwconfig pads the ESSID field; stripping the quotes before the spaces
// left `Freebox-20BC32"  ` in the ssid tag (seen on a Raspberry Pi).
func TestParseESSID(t *testing.T) {
	for line, want := range map[string]string{
		`wlan0     IEEE 802.11  ESSID:"Freebox-20BC32"  `:   "Freebox-20BC32",
		`wlan0     IEEE 802.11  ESSID:"Home Net"`:           "Home Net",
		`wlan0     unassociated  Nickname:"<WIFI@REALTEK>"`: "",
	} {
		if got := parseESSID(line); got != want {
			t.Errorf("parseESSID(%q) = %q, want %q", line, got, want)
		}
	}
}

// Outputs captured on a Raspberry Pi 5 running Debian 13, where iw ships
// with the image and iwconfig does not.
const iwDevPi = "phy#0\n\tUnnamed/non-netdev interface\n\t\twdev 0x3\n\t\taddr xx:xx:xx:xx:xx:xx\n\t\ttype P2P-device\n\t\ttxpower 31.00 dBm\n\tInterface wlan0\n\t\tifindex 3\n\t\twdev 0x1\n\t\taddr xx:xx:xx:xx:xx:xx\n\t\tssid Freebox-20BC32\n\t\ttype managed\n\t\tchannel 11 (2462 MHz), width: 20 MHz, center1: 2462 MHz\n\t\ttxpower 31.00 dBm\n"

const iwLinkPi = "Connected to aa:bb:cc:dd:ee:ff (on wlan0)\n\tSSID: Freebox-20BC32\n\tfreq: 2462.0\n\tRX: 769140575 bytes (540505 packets)\n\tTX: 30714204 bytes (173891 packets)\n\tsignal: -62 dBm\n\trx bitrate: 65.0 MBit/s\n\ttx bitrate: 72.2 MBit/s\n\tbss flags: short-slot-time\n\tdtim period: 2\n\tbeacon int: 120\n"

func TestParseIw(t *testing.T) {
	if got := parseIwDevInterfaces(iwDevPi); len(got) != 1 || got[0] != "wlan0" {
		t.Errorf("interfaces = %v, want [wlan0] (the P2P device has no name)", got)
	}
	ssid, bssid, dbm, ok := parseIwLink(iwLinkPi)
	if !ok || ssid != "Freebox-20BC32" || bssid != "aa:bb:cc:dd:ee:ff" || dbm != -62 {
		t.Errorf("parseIwLink = %q %q %d %v", ssid, bssid, dbm, ok)
	}
	if _, _, _, ok := parseIwLink("Not connected.\n"); ok {
		t.Error("a disconnected interface parsed as connected")
	}
	// iwconfig on the same link printed "Link Quality=48/70 Signal level=-62 dBm".
	if q := qualityFromDBm(-62); q < 68.5 || q > 68.6 {
		t.Errorf("qualityFromDBm(-62) = %v, want 48/70 = 68.57%%", q)
	}
}
