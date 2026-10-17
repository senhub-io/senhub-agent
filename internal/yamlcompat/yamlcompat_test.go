package yamlcompat_test

import (
	"reflect"
	"testing"
	"time"

	yamlv2 "gopkg.in/yaml.v2"
	yamlv3 "gopkg.in/yaml.v3"

	"senhub-agent.go/internal/yamlcompat"
)

type destination struct {
	Name    string
	Enabled *bool
	Params  map[string]interface{} `yaml:"params"`
	Tags    map[string]string      `yaml:"custom_tags"`
	Nested  struct{ On bool }
}

type rootSequence []map[string]interface{}

type testCase struct {
	name    string
	yaml    string
	newOut  func() interface{}
	prefill func(interface{})
	wantErr bool
}

func TestUnmarshalMatchesYAMLV2(t *testing.T) {
	newDest := func() interface{} { return &destination{} }
	prefillDest := func(out interface{}) {
		d := out.(*destination)
		d.Name = "keep"
		truth := true
		d.Enabled = &truth
		d.Params = map[string]interface{}{"existing": "value"}
		d.Tags = map[string]string{"existing": "tag"}
		d.Nested.On = true
	}

	cases := []testCase{
		{
			name: "bool words in interface values and keys",
			yaml: `params:
  y_val: y
  yes_val: yes
  on_val: on
  off_val: off
  n_val: n
  no_val: no
  nested:
    yes: from-key
    on: from-key
  list: [yes, off, no, n]
  flow: {y: flow-key, yes: flow-key}
`,
			newOut: newDest,
		},
		{
			name:   "quoted bool word in interface",
			yaml:   "params:\n  quoted_yes: \"yes\"\n",
			newOut: newDest,
		},
		{
			name: "timestamps in interface",
			yaml: `params:
  ts_date: 2024-01-02
  ts_iso: 2024-01-02T03:04:05Z
  ts_space: 2024-01-02 03:04:05
  ts_quoted: "2024-01-02"
`,
			newOut: newDest,
		},
		{
			name: "duplicate keys",
			yaml: `name: first
name: second
params:
  a: 1
  a: 2
  list:
    - {x: first, x: second}
`,
			newOut: newDest,
		},
		{
			name: "merge key",
			yaml: `base: &base
  a: yes
  b: no
params:
  <<: *base
  c: 3
`,
			newOut: newDest,
		},
		{
			name:   "typed plain yes",
			yaml:   "enabled: yes\n",
			newOut: newDest,
		},
		{
			name:    "typed quoted no becomes error",
			yaml:    "enabled: \"no\"\n",
			newOut:  newDest,
			wantErr: true,
		},
		{
			name: "string map values",
			yaml: `custom_tags:
  a: yes
  b: 5
  c: 1.5
  d: 0x1F
  e: null
`,
			newOut: newDest,
		},
		{
			name:    "empty document leaves destination untouched",
			yaml:    "",
			newOut:  newDest,
			prefill: prefillDest,
		},
		{
			name:    "comment only leaves destination untouched",
			yaml:    "# comment\n",
			newOut:  newDest,
			prefill: prefillDest,
		},
		{
			name: "scalar forms",
			yaml: `params:
  int: 42
  octal: 0755
  octal_o: 0o17
  hex: 0x1F
  float: 3.14
  inf: .inf
  tilde: ~
  null: null
`,
			newOut: newDest,
		},
		{
			name:   "typed bool words",
			yaml:   "enabled: Y\nnested:\n  on: off\n",
			newOut: newDest,
		},
		{
			name:   "null key in a string-keyed map",
			yaml:   "params:\n  ~: kept\n  other: 1\n",
			newOut: newDest,
		},
		{
			name:   "bool words colliding as keys",
			yaml:   "params:\n  nested:\n    yes: 1\n    on: 2\n    k: 3\n",
			newOut: newDest,
		},
		{
			name: "root sequence",
			yaml: `- a: 1
  b: yes
- c: "2024-01-02"
  d: 2024-01-02
- {x: first, x: second}
`,
			newOut: func() interface{} { return &rootSequence{} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v2Out := tc.newOut()
			if tc.prefill != nil {
				tc.prefill(v2Out)
			}
			v2Err := yamlv2.Unmarshal([]byte(tc.yaml), v2Out)

			compatOut := tc.newOut()
			if tc.prefill != nil {
				tc.prefill(compatOut)
			}
			compatErr := yamlcompat.Unmarshal([]byte(tc.yaml), compatOut)

			if tc.wantErr {
				if (v2Err == nil) != (compatErr == nil) {
					t.Fatalf("error mismatch: v2 err=%v compat err=%v", v2Err, compatErr)
				}
				if v2Err != nil && compatErr != nil && v2Err.Error() != compatErr.Error() {
					t.Fatalf("error text mismatch:\n v2: %s\n compat: %s", v2Err, compatErr)
				}
				return
			}

			if v2Err != nil || compatErr != nil {
				t.Fatalf("unexpected error: v2=%v compat=%v", v2Err, compatErr)
			}

			if tc.prefill != nil {
				expected := tc.newOut()
				tc.prefill(expected)
				if !reflect.DeepEqual(normalize(expected), normalize(v2Out)) || !reflect.DeepEqual(normalize(expected), normalize(compatOut)) {
					t.Fatalf("prefilled destination was modified:\n expected=%#v\n v2=%#v\n compat=%#v", normalize(expected), normalize(v2Out), normalize(compatOut))
				}
			}

			if !reflect.DeepEqual(normalize(v2Out), normalize(compatOut)) {
				t.Fatalf("decoded values differ:\n v2=%#v\n compat=%#v", normalize(v2Out), normalize(compatOut))
			}
		})
	}
}

func TestYAMLV3AloneDiffersDuplicateKeys(t *testing.T) {
	data := []byte("a: 1\na: 2\n")

	var v2Out map[string]interface{}
	if err := yamlv2.Unmarshal(data, &v2Out); err != nil {
		t.Fatalf("yaml.v2 failed: %v", err)
	}

	var v3Out map[string]interface{}
	if err := yamlv3.Unmarshal(data, &v3Out); err == nil {
		t.Fatalf("yaml.v3 unexpectedly accepted duplicate keys")
	}

	var compatOut map[string]interface{}
	if err := yamlcompat.Unmarshal(data, &compatOut); err != nil {
		t.Fatalf("yamlcompat failed: %v", err)
	}
	if !reflect.DeepEqual(normalize(v2Out), normalize(compatOut)) {
		t.Fatalf("yamlcompat does not match yaml.v2 after duplicate key removal:\n v2=%#v\n compat=%#v", normalize(v2Out), normalize(compatOut))
	}
}

func TestYAMLV3AloneDiffersBoolWordsInInterface(t *testing.T) {
	data := []byte("v: yes\n")

	var v2Out, v3Out map[string]interface{}
	if err := yamlv2.Unmarshal(data, &v2Out); err != nil {
		t.Fatalf("yaml.v2 failed: %v", err)
	}
	if err := yamlv3.Unmarshal(data, &v3Out); err != nil {
		t.Fatalf("yaml.v3 failed: %v", err)
	}

	if _, ok := v2Out["v"].(bool); !ok {
		t.Fatalf("yaml.v2 did not decode yes as bool: %#v", v2Out)
	}
	if v3Out["v"] != "yes" {
		t.Fatalf("yaml.v3 decoded yes as %T %#v, want string", v3Out["v"], v3Out["v"])
	}

	var compatOut map[string]interface{}
	if err := yamlcompat.Unmarshal(data, &compatOut); err != nil {
		t.Fatalf("yamlcompat failed: %v", err)
	}
	if !reflect.DeepEqual(normalize(v2Out), normalize(compatOut)) {
		t.Fatalf("yamlcompat does not match yaml.v2 for bool words:\n v2=%#v\n compat=%#v", normalize(v2Out), normalize(compatOut))
	}
}

func TestYAMLV3AloneDiffersTimestampsInInterface(t *testing.T) {
	data := []byte("v: 2024-01-02\n")

	var v2Out, v3Out map[string]interface{}
	if err := yamlv2.Unmarshal(data, &v2Out); err != nil {
		t.Fatalf("yaml.v2 failed: %v", err)
	}
	if err := yamlv3.Unmarshal(data, &v3Out); err != nil {
		t.Fatalf("yaml.v3 failed: %v", err)
	}

	if v2Out["v"] != "2024-01-02" {
		t.Fatalf("yaml.v2 decoded timestamp as %T %#v, want string", v2Out["v"], v2Out["v"])
	}
	if _, ok := v3Out["v"].(time.Time); !ok {
		t.Fatalf("yaml.v3 decoded timestamp as %T %#v, want time.Time", v3Out["v"], v3Out["v"])
	}

	var compatOut map[string]interface{}
	if err := yamlcompat.Unmarshal(data, &compatOut); err != nil {
		t.Fatalf("yamlcompat failed: %v", err)
	}
	if compatOut["v"] != "2024-01-02" {
		t.Fatalf("yamlcompat decoded timestamp as %T %#v, want string", compatOut["v"], compatOut["v"])
	}
}

func TestYAMLV3AloneDiffersQuotedBoolIntoBool(t *testing.T) {
	data := []byte("enabled: \"no\"\n")
	type dst struct {
		Enabled *bool `yaml:"enabled"`
	}

	var v2Out dst
	v2Err := yamlv2.Unmarshal(data, &v2Out)
	if v2Err == nil {
		t.Fatalf("yaml.v2 accepted quoted no into bool")
	}

	var v3Out dst
	v3Err := yamlv3.Unmarshal(data, &v3Out)
	if v3Err != nil {
		t.Fatalf("yaml.v3 rejected quoted no into bool: %v", v3Err)
	}
	if v3Out.Enabled == nil || *v3Out.Enabled != false {
		t.Fatalf("yaml.v3 decoded quoted no as %#v, want false", v3Out.Enabled)
	}

	var compatOut dst
	compatErr := yamlcompat.Unmarshal(data, &compatOut)
	if compatErr == nil {
		t.Fatalf("yamlcompat accepted quoted no into bool")
	}
	if v2Err.Error() != compatErr.Error() {
		t.Fatalf("error text mismatch:\n v2: %s\n compat: %s", v2Err, compatErr)
	}
}

func normalize(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	return normalizeValue(reflect.ValueOf(v))
}

func normalizeValue(v reflect.Value) interface{} {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return normalizeValue(v.Elem())
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return normalizeValue(v.Elem())
	case reflect.Struct:
		m := make(map[string]interface{}, v.NumField())
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			m[t.Field(i).Name] = normalizeValue(v.Field(i))
		}
		return m
	case reflect.Map:
		m := make(map[string]interface{}, v.Len())
		for _, k := range v.MapKeys() {
			key := k
			if key.Kind() == reflect.Interface {
				if key.IsNil() {
					continue
				}
				key = key.Elem()
			}
			if key.Kind() != reflect.String {
				continue
			}
			m[key.String()] = normalizeValue(v.MapIndex(k))
		}
		return m
	case reflect.Slice:
		s := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			s[i] = normalizeValue(v.Index(i))
		}
		return s
	default:
		return v.Interface()
	}
}

func TestMarshalUsesTwoSpaceIndentAndRoundTrips(t *testing.T) {
	in := map[string]interface{}{
		"name":  "x",
		"words": []interface{}{"yes", "a: b", map[string]interface{}{"k": []interface{}{1, 2}}},
		"deep":  map[string]interface{}{"a": map[string]interface{}{"b": "c"}},
	}
	body, err := yamlcompat.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "deep:\n  a:\n    b: c\nname: x\nwords:\n  - \"yes\"\n  - 'a: b'\n  - k:\n      - 1\n      - 2\n"
	if string(body) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", body, want)
	}

	var out map[string]interface{}
	if err := yamlcompat.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalize(in), normalize(out)) {
		t.Fatalf("round trip changed the document:\n in=%#v\n out=%#v", normalize(in), normalize(out))
	}
}
