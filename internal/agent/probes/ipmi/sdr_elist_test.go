package ipmi

import (
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
)

// realElistOutput is `ipmitool sdr elist full` as a Dell PowerEdge BMC
// prints it: name, sensor number, status, entity, reading. Fan5B and
// Fan6B are absent (a half-populated fan board): ipmitool reports them
// "ns" with "No Reading".
const realElistOutput = `Inlet Temp       | 04h | ok  |  7.1 | 23 degrees C
Exhaust Temp     | 01h | ok  |  7.1 | 38 degrees C
Fan1A            | 30h | ok  |  7.1 | 5040 RPM
Fan5B            | 3Bh | ns  |  7.1 | No Reading
Fan6B            | 3Dh | ns  |  7.1 | No Reading
Voltage 1        | 6Ah | ok  | 10.1 | 232 Volts
Current 1        | 6Eh | ok  | 10.1 | 0.80 Amps
Pwr Consumption  | 76h | ok  |  7.1 | 168 Watts
Temp             | 0Fh | cr  |  3.2 | 97 degrees C
`

func collectOutput(t *testing.T, output string) []data_store.DataPoint {
	t.Helper()
	p := &ipmiProbe{
		BaseProbe:    newBaseProbe(),
		cfg:          ipmiConfig{Mode: "local", IpmitoolPath: defaultIpmitoolPath, Interval: defaultInterval},
		moduleLogger: newTestModuleLogger(t),
		runner:       func(_ ipmiConfig) (string, error) { return output, nil },
	}
	p.SetProbeType(ProbeType)
	p.SetName("test-ipmi")
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	return points
}

func pointsOf(points []data_store.DataPoint, component string) map[string]float64 {
	out := map[string]float64{}
	for _, dp := range points {
		for _, tag := range dp.Tags {
			if tag.Key == "hardware.component" && tag.Value == component {
				out[dp.Name] = dp.Value
			}
		}
	}
	return out
}

// The probe runs `sdr elist full`, whose second field is the sensor
// number: read as the reading, it hid every temperature, fan speed and
// voltage behind a status point.
func TestCollect_ElistFullCarriesTheReadings(t *testing.T) {
	points := collectOutput(t, realElistOutput)

	cases := []struct {
		component, metric string
		want              float64
	}{
		{"Inlet Temp", "hardware.temperature", 23},
		{"Fan1A", "hardware.fan.speed", 5040},
		{"Voltage 1", "hardware.voltage", 232},
		{"Inlet Temp", "hardware.sensor.status", 1},
		{"Pwr Consumption", "hardware.power_supply.status", 1},
	}
	for _, tc := range cases {
		got, ok := pointsOf(points, tc.component)[tc.metric]
		if !ok || got != tc.want {
			t.Errorf("%s %s = %v (present %v), want %v", tc.component, tc.metric, got, ok, tc.want)
		}
	}
}

func TestParseSdrOutput_ElistLayout(t *testing.T) {
	rows := parseSdrOutput("Fan1A            | 30h | ok  |  7.1 | 5040 RPM\n")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	want := sensorRow{name: "Fan1A", value: "5040 RPM", status: "ok"}
	if rows[0] != want {
		t.Errorf("row = %+v, want %+v", rows[0], want)
	}
}
