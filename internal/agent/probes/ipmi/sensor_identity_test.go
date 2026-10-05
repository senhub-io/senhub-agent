package ipmi

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

// dellElist is `ipmitool sdr elist full` from a two-socket Dell PowerEdge
// (iDRAC): every CPU temperature is called "Temp", told apart only by the
// entity instance (3.1, 3.2). Lines are modelled on that layout; the
// values are made up.
const dellElist = `Inlet Temp       | 04h | ok  |  7.1 | 23 degrees C
Temp             | 0Eh | ok  |  3.1 | 61 degrees C
Temp             | 0Fh | ok  |  3.2 | 58 degrees C
Fan1A            | 30h | ok  |  7.1 | 5040 RPM
Current 1        | 6Ah | ok  | 10.1 | 0.80 Amps
Current 2        | 6Bh | ok  | 10.2 | 0.60 Amps
Voltage 1        | 6Ch | ok  | 10.1 | 232 Volts
Pwr Consumption  | 76h | ok  |  7.1 | 168 Watts
PS1 Status       | 70h | ok  | 10.1 | Presence detected
PS2 Status       | 71h | ok  | 10.2 | Presence detected
PS Redundancy    | 77h | ok  |  7.1 | Fully Redundant
`

// hpeElist and supermicroElist are INVENTED from memory of the vendors'
// naming, not captured from a BMC: HPE numbers its CPU and PSU sensors in
// the name, Supermicro leaves PSU sensors on entity 10.x. They exercise
// the parser, they prove nothing about a real iLO or Supermicro BMC.
const hpeElist = `Temp 1           | 01h | ok  |  3.1 | 40 degrees C
Temp 2           | 02h | ok  |  3.2 | 41 degrees C
Power Supply 1   | 03h | ok  | 10.1 | Presence detected
Power Supply 2   | 04h | cr  | 10.2 | Failure detected
Power Meter      | 05h | ok  |  7.1 | 160 Watts
`

const supermicroElist = `CPU1 Temp        | 01h | ok  |  3.1 | 42 degrees C
PS1 Status       | C8h | ok  | 10.1 | Presence detected
PS2 Status       | C9h | cr  | 10.2 | Presence detected
PS2 Redundancy   | CAh | cr  | 21.1 | Redundancy Lost
PS3 Status       | CBh | ok  | 10.3 | Device Absent
`

func componentsOf(points []data_store.DataPoint, metric string) map[string]float64 {
	out := map[string]float64{}
	for _, dp := range points {
		if dp.Name != metric {
			continue
		}
		for _, tag := range dp.Tags {
			if tag.Key == "hardware.component" {
				out[tag.Value] = dp.Value
			}
		}
	}
	return out
}

func TestCollect_SameNamedSensorsStayDistinct(t *testing.T) {
	points := collectOutput(t, dellElist)

	temps := componentsOf(points, "hardware.temperature")
	want := map[string]float64{"Inlet Temp": 23, "Temp (CPU 1)": 61, "Temp (CPU 2)": 58}
	for name, v := range want {
		if got, ok := temps[name]; !ok || got != v {
			t.Errorf("temperature %q = %v (present %v), want %v; got %v", name, got, ok, v, temps)
		}
	}
	if _, collapsed := temps["Temp"]; collapsed {
		t.Errorf("a duplicated name must not keep the bare name: %v", temps)
	}

	for _, dp := range points {
		var comp string
		extra := false
		for _, tag := range dp.Tags {
			if tag.Key == "hardware.component" {
				comp = tag.Value
			}
			if tag.Key == "hardware.entity" || tag.Key == "hardware.sensor_number" {
				extra = true
			}
		}
		if comp == "Inlet Temp" && extra {
			t.Error("a sensor whose name is unique must keep its series unchanged, without the extra tags")
		}
	}
}

func TestCollect_DuplicatedSensorsCarryEntityAndNumber(t *testing.T) {
	points := collectOutput(t, dellElist)
	found := map[string]string{}
	for _, dp := range points {
		if dp.Name != "hardware.temperature" {
			continue
		}
		var comp, ent, num string
		for _, tag := range dp.Tags {
			switch tag.Key {
			case "hardware.component":
				comp = tag.Value
			case "hardware.entity":
				ent = tag.Value
			case "hardware.sensor_number":
				num = tag.Value
			}
		}
		found[comp] = ent + "/" + num
	}
	if found["Temp (CPU 1)"] != "3.1/0Eh" || found["Temp (CPU 2)"] != "3.2/0Fh" {
		t.Errorf("entity/sensor tags = %v", found)
	}
}

// Every output keys its series on the registry's tags: two sensors that
// share a name must differ on at least one of them.
func TestDuplicatedSensors_DifferOnADiscriminantTag(t *testing.T) {
	disc := http.DiscriminantTagsRegistry[ProbeType]
	if len(disc) == 0 {
		t.Fatal("ipmi is not in DiscriminantTagsRegistry")
	}
	listings := map[string]string{
		"dell":       dellElist,
		"hpe":        hpeElist,
		"supermicro": supermicroElist,
		"same entity": "Temp | 01h | ok | 3.1 | 40 degrees C\nTemp | 02h | ok | 3.1 | 41 degrees C\n" +
			"Temp | 03h | ok | 3.1 | 42 degrees C\n",
		"plain": "Temp | 40 degrees C | ok\nTemp | 41 degrees C | ok\n",
	}
	for name, elist := range listings {
		keys := map[string]bool{}
		for _, dp := range collectOutput(t, elist) {
			if dp.Name != "hardware.temperature" && dp.Name != "hardware.sensor.status" {
				continue
			}
			tagMap := map[string]string{}
			for _, tag := range dp.Tags {
				tagMap[tag.Key] = tag.Value
			}
			var parts []string
			for _, k := range disc {
				parts = append(parts, tagMap[k])
			}
			key := dp.Name + "|" + strings.Join(parts, "|")
			if keys[key] {
				t.Errorf("%s: two series share the key %q", name, key)
			}
			keys[key] = true
		}
	}
}

func TestCollect_PowerAndCurrentReadings(t *testing.T) {
	points := collectOutput(t, dellElist)

	if got := componentsOf(points, "hardware.power")["Pwr Consumption"]; got != 168 {
		t.Errorf("power = %v, want 168", got)
	}
	cur := componentsOf(points, "senhub.hardware.current")
	if cur["Current 1"] != 0.8 || cur["Current 2"] != 0.6 {
		t.Errorf("current = %v", cur)
	}
	psu := componentsOf(points, "hardware.power_supply.status")
	if got := psu["Current 1"]; got != 1 {
		t.Errorf("PSU current sensor status = %v, want 1", got)
	}
	if _, ok := psu["Pwr Consumption"]; ok {
		t.Error("the machine's consumption is not a power supply")
	}
}

func TestCollect_PowerSupplyPresenceAndRedundancy(t *testing.T) {
	psu := componentsOf(collectOutput(t, dellElist), "hardware.power_supply.status")
	for _, name := range []string{"PS1 Status", "PS2 Status", "PS Redundancy"} {
		if got, ok := psu[name]; !ok || got != 1 {
			t.Errorf("%s = %v (present %v), want 1", name, got, ok)
		}
	}

	hpe := componentsOf(collectOutput(t, hpeElist), "hardware.power_supply.status")
	if hpe["Power Supply 1"] != 1 || hpe["Power Supply 2"] != 0 {
		t.Errorf("hpe PSU status = %v", hpe)
	}

	smPoints := collectOutput(t, supermicroElist)
	sm := componentsOf(smPoints, "hardware.power_supply.status")
	if sm["PS1 Status"] != 1 {
		t.Errorf("PS1 = %v", sm["PS1 Status"])
	}
	if sm["PS2 Status"] != 0 {
		t.Errorf("PS2 (status cr) = %v, want 0", sm["PS2 Status"])
	}
	if got, ok := sm["PS2 Redundancy"]; !ok || got != 0 {
		t.Errorf("redundancy lost = %v (present %v), want 0", got, ok)
	}
	if got := pointsOf(smPoints, "PS3 Status"); len(got) != 0 {
		t.Errorf("an absent supply is not a fault and must emit nothing, got %v", got)
	}
}

func TestDiscreteState(t *testing.T) {
	cases := map[string]struct {
		kind  discreteKind
		known bool
	}{
		"Presence detected":   {stateGood, true},
		"Presence Detected":   {stateGood, true},
		"Device Present":      {stateGood, true},
		"Fully Redundant":     {stateGood, true},
		"Redundancy Lost":     {stateBad, true},
		"Redundancy Degraded": {stateBad, true},
		"Non-redundant":       {stateBad, true},
		"Failure detected":    {stateBad, true},
		"Device Absent":       {stateAbsent, true},
		"State Deasserted":    {0, false},
		"":                    {0, false},
	}
	for in, want := range cases {
		kind, known := discreteState(in)
		if known != want.known || (known && kind != want.kind) {
			t.Errorf("discreteState(%q) = %v,%v want %v,%v", in, kind, known, want.kind, want.known)
		}
	}
}

func TestDisambiguateNames_Fallbacks(t *testing.T) {
	rows := disambiguateNames(parseSdrOutput(
		"Temp | 01h | ok | 3.1 | 40 degrees C\nTemp | 02h | ok | 3.1 | 41 degrees C\n" +
			"Fan | 03h | ok | 99.4 | 100 RPM\nFan | 04h | ok | 99.5 | 100 RPM\n"))
	want := []string{"Temp (CPU 1)", "Temp (CPU 1 02h)", "Fan (99.4)", "Fan (99.5)"}
	for i := range want {
		if rows[i].displayName != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i].displayName, want[i])
		}
	}
}

// Every metric name the probe can emit needs a definition, or the sinks
// drop it silently.
func TestEveryEmittedNameHasADefinition(t *testing.T) {
	defs, err := transformers.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, d := range defs {
		if d.ProbeName != ProbeType {
			continue
		}
		for _, m := range d.Metrics {
			declared[m.Name] = true
		}
	}
	for _, elist := range []string{dellElist, hpeElist, supermicroElist, realElistOutput, sampleSdrOutput} {
		for _, dp := range collectOutput(t, elist) {
			if !declared[dp.Name] {
				t.Errorf("emitted %q has no definition in ipmi.yaml", dp.Name)
			}
		}
	}
}
