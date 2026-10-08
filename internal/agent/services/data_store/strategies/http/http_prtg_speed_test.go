package http

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func speedChannel(t *testing.T, probe, metricName, displayUnit string, value float64, native bool) string {
	t.Helper()
	converter, _ := newSemanticsHarness(t)
	metric := CachedMetric{
		Value:      value,
		Unit:       displayUnit,
		MetricName: metricName,
		ProbeName:  probe,
		Tags:       map[string]string{"probe_type": probe, "interface": "1/1"},
	}
	ch := converter.transformToPRTGChannelWithFilter(metricName, metric, MetricFilter{ShowTags: true, NativeSpeed: native})
	if ch == nil {
		t.Fatalf("nil channel for %s/%s", probe, metricName)
	}
	ch.Channel = "c"
	out, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

func TestPRTGSpeedUnits(t *testing.T) {
	cases := []struct {
		name    string
		probe   string
		metric  string
		display string
		value   float64
		native  bool
		want    string
	}{
		{"netscaler rx Mbit/s default", "netscaler", "netscaler.interface.rx.mbits_per_sec", "Mbits/s", 1588, false,
			`{"channel":"c","value":1588,"float":1,"unit":"Custom","customunit":"Mbit/s"}`},
		{"netscaler rx Mbit/s native", "netscaler", "netscaler.interface.rx.mbits_per_sec", "Mbits/s", 1588, true,
			`{"channel":"c","value":198500000,"float":1,"unit":"SpeedNet"}`},
		{"host network bit/s default", "network", "interface_speed", "bit/s", 10000000000, false,
			`{"channel":"c","value":10000000000,"float":1,"unit":"Custom","customunit":"bit/s"}`},
		{"host network bit/s native", "network", "interface_speed", "bit/s", 10000000000, true,
			`{"channel":"c","value":1250000000,"float":1,"unit":"SpeedNet"}`},
		{"byte rate default", "network", "bytes_received", "Bytes/s", 5000, false,
			`{"channel":"c","value":5000,"float":1,"unit":"SpeedNet"}`},
		{"byte rate native", "network", "bytes_received", "Bytes/s", 5000, true,
			`{"channel":"c","value":5000,"float":1,"unit":"SpeedNet"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := speedChannel(t, tc.probe, tc.metric, tc.display, tc.value, tc.native); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestBitRateOfScales(t *testing.T) {
	cases := []struct {
		unit    string
		label   string
		perByte float64
	}{
		{"bps", "bit/s", 0.125},
		{"kbit/s", "kbit/s", 125},
		{"Mbps", "Mbit/s", 125000},
		{"Gbit/s", "Gbit/s", 125000000},
	}
	for _, tc := range cases {
		br, ok := bitRateOf("", tc.unit)
		if !ok || br.label != tc.label || br.bytesPerSecBy != tc.perByte {
			t.Errorf("%s: got %+v ok=%v", tc.unit, br, ok)
		}
	}
	if _, ok := bitRateOf("By/s", "Bytes/s"); ok {
		t.Error("byte rate must not be a bit rate")
	}
}

func TestNativeSpeedRequested(t *testing.T) {
	for query, want := range map[string]bool{
		"":             false,
		"speed=native": true,
		"speed=NATIVE": true,
		"speed=bogus":  false,
		"speed=":       false,
		"speed=bytes":  false,
	} {
		r := httptest.NewRequest("GET", "/api/k/prtg/metrics/p?"+query, nil)
		if got := NativeSpeedRequested(r); got != want {
			t.Errorf("%q: got %v, want %v", query, got, want)
		}
		m := &MetricsProcessor{}
		if got := m.ParseMetricFilter(r).NativeSpeed; got != want {
			t.Errorf("ParseMetricFilter %q: got %v, want %v", query, got, want)
		}
	}
}
