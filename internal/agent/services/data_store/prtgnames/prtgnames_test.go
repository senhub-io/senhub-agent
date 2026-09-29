package prtgnames

import "testing"

// PRTG keeps one value per channel name. Series whose display name did not
// carry what sets them apart (a scraped exporter's labels) collapsed into
// one channel and lost their values.
func TestDisambiguate(t *testing.T) {
	names := []string{"Node CPU Seconds", "Node CPU Seconds", "Node CPU Seconds", "Uptime", "Twin", "Twin"}
	tags := []map[string]string{
		{"probe_name": "ne", "cpu": "0", "mode": "idle"},
		{"probe_name": "ne", "cpu": "0", "mode": "user"},
		{"probe_name": "ne", "cpu": "1", "mode": "idle"},
		{"probe_name": "ne"},
		{"probe_name": "ne"},
		{"probe_name": "ne"},
	}
	got := Disambiguate(names, tags)
	want := []string{
		"Node CPU Seconds (cpu=0, mode=idle)",
		"Node CPU Seconds (cpu=0, mode=user)",
		"Node CPU Seconds (cpu=1, mode=idle)",
		"Uptime",
		"Twin (#1)",
		"Twin (#2)",
	}
	seen := map[string]bool{}
	for i, n := range got {
		if n != want[i] {
			t.Errorf("channel %d = %q, want %q", i, n, want[i])
		}
		if seen[n] {
			t.Errorf("duplicate channel name %q", n)
		}
		seen[n] = true
	}
}
