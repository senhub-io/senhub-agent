package app

import "testing"

func TestProcessPerPIDWarning(t *testing.T) {
	filter := map[string]interface{}{"top_n": 10}
	detail := map[string]interface{}{"per_pid": true}
	cases := []struct {
		name      string
		probeType string
		params    map[string]interface{}
		want      bool
	}{
		{"top_n with per_pid", "process", map[string]interface{}{"filter": filter, "detail": detail}, true},
		{"top_n alone", "process", map[string]interface{}{"filter": filter}, false},
		{"per_pid without top_n", "process", map[string]interface{}{"detail": detail}, false},
		{"other probe", "mysql", map[string]interface{}{"filter": filter, "detail": detail}, false},
	}
	for _, c := range cases {
		if got := processPerPIDWarning(c.probeType, c.params) != ""; got != c.want {
			t.Errorf("%s: warning=%v, want %v", c.name, got, c.want)
		}
	}
}
