package types

import (
	"reflect"
	"testing"
	"time"
)

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

func TestStringParam(t *testing.T) {
	cases := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal string
		wantOK  bool
	}{
		{"string literal", map[string]interface{}{"host": "db1"}, "host", "db1", true},
		{"empty string is a value", map[string]interface{}{"host": ""}, "host", "", true},
		{"missing key", map[string]interface{}{}, "host", "", false},
		{"number rejected", map[string]interface{}{"host": 10}, "host", "", false},
		{"bool rejected", map[string]interface{}{"host": true}, "host", "", false},
		{"nil rejected", map[string]interface{}{"host": nil}, "host", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := StringParam(tc.m, tc.key)
			if got != tc.wantVal || ok != tc.wantOK {
				t.Errorf("StringParam(%v, %q) = (%q, %v), want (%q, %v)",
					tc.m, tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestBoolParam(t *testing.T) {
	cases := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal bool
		wantOK  bool
	}{
		{"yaml bool literal", map[string]interface{}{"tls": true}, "tls", true, true},
		{"yaml false literal", map[string]interface{}{"tls": false}, "tls", false, true},
		{"string true", map[string]interface{}{"tls": "true"}, "tls", true, true},
		{"string 0", map[string]interface{}{"tls": "0"}, "tls", false, true},
		{"padded string", map[string]interface{}{"tls": " TRUE "}, "tls", true, true},
		{"missing key", map[string]interface{}{}, "tls", false, false},
		{"non-bool string rejected", map[string]interface{}{"tls": "maybe"}, "tls", false, false},
		{"number rejected", map[string]interface{}{"tls": 1}, "tls", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := BoolParam(tc.m, tc.key)
			if got != tc.wantVal || ok != tc.wantOK {
				t.Errorf("BoolParam(%v, %q) = (%v, %v), want (%v, %v)",
					tc.m, tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestStringSlice(t *testing.T) {
	cases := []struct {
		name string
		raw  interface{}
		want []string
	}{
		{"yaml list", []interface{}{"a", "b"}, []string{"a", "b"}},
		{"yaml list drops empties and non-strings", []interface{}{"a", "", 3, nil, "b"}, []string{"a", "b"}},
		{"native slice", []string{"a", ""}, []string{"a"}},
		{"lone scalar", "a", []string{"a"}},
		{"empty scalar", "", nil},
		{"empty list", []interface{}{}, []string{}},
		{"nil", nil, nil},
		{"map rejected", map[string]interface{}{"a": 1}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StringSlice(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("StringSlice(%#v) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestStringSliceParam(t *testing.T) {
	cases := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal []string
		wantOK  bool
	}{
		{"yaml list", map[string]interface{}{"paths": []interface{}{"a"}}, "paths", []string{"a"}, true},
		{"lone scalar", map[string]interface{}{"paths": "a"}, "paths", []string{"a"}, true},
		{"empty list is present", map[string]interface{}{"paths": []interface{}{}}, "paths", []string{}, true},
		{"missing key", map[string]interface{}{}, "paths", nil, false},
		{"wrong shape rejected", map[string]interface{}{"paths": 42}, "paths", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := StringSliceParam(tc.m, tc.key)
			if !reflect.DeepEqual(got, tc.wantVal) || ok != tc.wantOK {
				t.Errorf("StringSliceParam(%v, %q) = (%#v, %v), want (%#v, %v)",
					tc.m, tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestDurationParam(t *testing.T) {
	cases := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal time.Duration
		wantOK  bool
	}{
		{"bare int is seconds", map[string]interface{}{"timeout": 45}, "timeout", 45 * time.Second, true},
		{"float64 seconds", map[string]interface{}{"timeout": float64(1.5)}, "timeout", 1500 * time.Millisecond, true},
		{"duration string", map[string]interface{}{"timeout": "2m"}, "timeout", 2 * time.Minute, true},
		{"numeric string is seconds", map[string]interface{}{"timeout": "45"}, "timeout", 45 * time.Second, true},
		{"missing key", map[string]interface{}{}, "timeout", 0, false},
		{"garbage string rejected", map[string]interface{}{"timeout": "soon"}, "timeout", 0, false},
		{"bool rejected", map[string]interface{}{"timeout": true}, "timeout", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DurationParam(tc.m, tc.key)
			if got != tc.wantVal || ok != tc.wantOK {
				t.Errorf("DurationParam(%v, %q) = (%v, %v), want (%v, %v)",
					tc.m, tc.key, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}
