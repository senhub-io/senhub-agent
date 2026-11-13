package operstate

import "testing"

func TestFromIFMIB(t *testing.T) {
	cases := map[int]string{
		1: Up,
		2: Down,
		3: Testing,
		4: Unknown,
		5: Dormant,
		6: NotPresent,
		7: LowerLayerDown,
	}
	for in, want := range cases {
		got, ok := FromIFMIB(in)
		if !ok || got != want {
			t.Errorf("FromIFMIB(%d) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	for _, in := range []int{0, 8, 99, -1} {
		if got, ok := FromIFMIB(in); ok {
			t.Errorf("FromIFMIB(%d) = %q, true; want omitted", in, got)
		}
	}
}

func TestFromLinuxSysfs(t *testing.T) {
	cases := map[string]string{
		"up":             Up,
		"down":           Down,
		"testing":        Testing,
		"unknown":        Unknown,
		"dormant":        Dormant,
		"notpresent":     NotPresent,
		"lowerlayerdown": LowerLayerDown,
		" Up\n":          Up, // trimmed + lowercased
		"DORMANT":        Dormant,
	}
	for in, want := range cases {
		got, ok := FromLinuxSysfs(in)
		if !ok || got != want {
			t.Errorf("FromLinuxSysfs(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "garbage", "not_present"} {
		if got, ok := FromLinuxSysfs(in); ok {
			t.Errorf("FromLinuxSysfs(%q) = %q, true; want omitted", in, got)
		}
	}
}
