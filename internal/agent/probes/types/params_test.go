package types

import "testing"

func TestIntParam(t *testing.T) {
	cases := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal int
		wantOK  bool
	}{
		{"yaml.v2 int literal", map[string]interface{}{"port": 514}, "port", 514, true},
		{"json float64 integer", map[string]interface{}{"port": float64(514)}, "port", 514, true},
		{"int32", map[string]interface{}{"port": int32(514)}, "port", 514, true},
		{"int64", map[string]interface{}{"port": int64(514)}, "port", 514, true},
		{"float64 integer", map[string]interface{}{"port": float64(514)}, "port", 514, true},
		{"string numeric", map[string]interface{}{"port": "514"}, "port", 514, true},
		{"missing key", map[string]interface{}{}, "port", 0, false},
		{"float with fractional part rejected", map[string]interface{}{"port": float64(514.5)}, "port", 0, false},
		{"non-numeric string rejected", map[string]interface{}{"port": "foo"}, "port", 0, false},
		{"bool rejected", map[string]interface{}{"port": true}, "port", 0, false},
		{"nil rejected", map[string]interface{}{"port": nil}, "port", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := IntParam(tc.m, tc.key)
			if got != tc.wantVal || ok != tc.wantOK {
				t.Errorf("IntParam(%v, %q) = (%d, %v), want (%d, %v)",
					tc.m, tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestFloatParam(t *testing.T) {
	cases := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal float64
		wantOK  bool
	}{
		{"float64 literal", map[string]interface{}{"f": 1.5}, "f", 1.5, true},
		{"int literal accepted", map[string]interface{}{"f": 2}, "f", 2.0, true},
		{"int64 accepted", map[string]interface{}{"f": int64(3)}, "f", 3.0, true},
		{"string numeric", map[string]interface{}{"f": "1.5"}, "f", 1.5, true},
		{"missing key", map[string]interface{}{}, "f", 0, false},
		{"non-numeric string rejected", map[string]interface{}{"f": "foo"}, "f", 0, false},
		{"bool rejected", map[string]interface{}{"f": false}, "f", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := FloatParam(tc.m, tc.key)
			if got != tc.wantVal || ok != tc.wantOK {
				t.Errorf("FloatParam(%v, %q) = (%g, %v), want (%g, %v)",
					tc.m, tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}
