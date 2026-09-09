package spec

import (
	"strings"
	"testing"
)

func sampleSpec() Probe {
	return Probe{
		Type: "sample", DisplayName: "Sample",
		Params: []ParamSpec{
			{Key: "host", Kind: KindString, Required: true},
			{Key: "port", Kind: KindInt, Default: 3306},
			{Key: "password", Kind: KindString, Secret: true},
			{Key: "sslmode", Kind: KindString, Enum: []string{"disable", "require"}},
			{Key: "levels", Kind: KindStringList, Enum: []string{"Error", "Warning"}},
			{Key: "timeout", Kind: KindDuration},
			{Key: "tls", Kind: KindBlock, Fields: []ParamSpec{
				{Key: "enabled", Kind: KindBool},
				{Key: "ca_file", Kind: KindString, AlsoAccepts: []string{"ca_cert"}},
			}},
		},
	}
}

func TestProbe_DeclaredKeys(t *testing.T) {
	keys := sampleSpec().DeclaredKeys()
	for _, want := range []string{"host", "port", "tls", "tls.enabled", "tls.ca_file", "tls.ca_cert"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("declared keys miss %q", want)
		}
	}
}

func TestProbe_CheckParams(t *testing.T) {
	spec := sampleSpec()
	cases := []struct {
		name   string
		params map[string]interface{}
		want   []string
	}{
		{"valid", map[string]interface{}{"host": "db", "port": 3306, "sslmode": "require", "levels": []interface{}{"Error"}, "timeout": "10s", "tls": map[string]interface{}{"enabled": true, "ca_cert": "/x"}}, nil},
		{"missing required", map[string]interface{}{"port": 1}, []string{"host: required"}},
		{"unknown key", map[string]interface{}{"host": "h", "hots": "h"}, []string{"hots: not a parameter"}},
		{"bad enum", map[string]interface{}{"host": "h", "sslmode": "maybe"}, []string{"sslmode: must be one of"}},
		{"bad int", map[string]interface{}{"host": "h", "port": "abc"}, []string{"port: must be a number"}},
		{"bad list item", map[string]interface{}{"host": "h", "levels": []interface{}{"Nope"}}, []string{"levels: items must be one of"}},
		{"nested unknown", map[string]interface{}{"host": "h", "tls": map[string]interface{}{"insecure": true}}, []string{"tls.insecure: not a parameter"}},
		{"yaml.v2 nested map", map[string]interface{}{"host": "h", "tls": map[interface{}]interface{}{"enabled": "yes"}}, []string{"tls.enabled: must be true or false"}},
		{"block as bool tolerated", map[string]interface{}{"host": "h", "tls": true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := spec.CheckParams(c.params)
			if len(c.want) == 0 && len(got) != 0 {
				t.Fatalf("unexpected problems: %v", got)
			}
			for _, w := range c.want {
				found := false
				for _, g := range got {
					if strings.Contains(g.String(), w) {
						found = true
					}
				}
				if !found {
					t.Errorf("want a problem containing %q, got %v", w, got)
				}
			}
		})
	}
}

func TestRegister(t *testing.T) {
	spec := Probe{Type: "zz-test-spec"}
	Register(spec)
	t.Cleanup(func() { unregister("zz-test-spec") })
	if _, ok := For("zz-test-spec"); !ok {
		t.Fatal("registered spec not found")
	}
	defer func() {
		if recover() == nil {
			t.Error("a duplicate registration must panic")
		}
	}()
	Register(spec)
}

func TestProbe_HasStartSet(t *testing.T) {
	if (Probe{Params: []ParamSpec{{Key: "interval", Kind: KindInt}}}).HasStartSet() {
		t.Error("no required or essential parameter must mean no start set")
	}
	if !(Probe{Params: []ParamSpec{{Key: "user", Kind: KindString, Essential: true}}}).HasStartSet() {
		t.Error("an essential parameter is a start set")
	}
	if !(Probe{Params: []ParamSpec{{Key: "v3", Kind: KindBlock, EssentialWhen: []Condition{{Key: "version", Values: []string{"v3"}}}}}}).HasStartSet() {
		t.Error("a conditional essential is a start set")
	}
}

func TestCheckParams_BoolForAStringEnum(t *testing.T) {
	sp := Probe{Type: "x", Params: []ParamSpec{{Key: "encrypt", Kind: KindString, Enum: []string{"true", "false", "disable"}}, {Key: "mode", Kind: KindString, Enum: []string{"a", "b"}}}}
	if problems := sp.CheckParams(map[string]interface{}{"encrypt": true}); len(problems) != 0 {
		t.Errorf("an unquoted true must pass an enum that lists it: %v", problems)
	}
	if problems := sp.CheckParams(map[string]interface{}{"mode": true}); len(problems) != 1 {
		t.Errorf("a bool for an enum without it is still a shape problem: %v", problems)
	}
}
