package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubstitute_EnvPresent(t *testing.T) {
	t.Setenv("SENHUB_TEST_FOO", "bar")
	got, err := substituteString("${env:SENHUB_TEST_FOO}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bar" {
		t.Errorf("got %q, want %q", got, "bar")
	}
}

func TestSubstitute_EnvDefault(t *testing.T) {
	// Make sure the env var is NOT set so the default branch fires.
	if err := os.Unsetenv("SENHUB_TEST_MISSING"); err != nil {
		t.Fatalf("unsetenv failed: %v", err)
	}
	got, err := substituteString("${env:SENHUB_TEST_MISSING:-fallback}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

func TestSubstitute_EnvMissingNoDefault(t *testing.T) {
	if err := os.Unsetenv("SENHUB_TEST_ABSENT"); err != nil {
		t.Fatalf("unsetenv failed: %v", err)
	}
	got, err := substituteString("[${env:SENHUB_TEST_ABSENT}]")
	if err != nil {
		t.Fatalf("missing env without default should NOT error, got %v", err)
	}
	if got != "[]" {
		t.Errorf("missing env should resolve to empty, got %q", got)
	}
}

func TestSubstitute_File(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "db_password")
	// Include a trailing newline to verify TrimSpace runs.
	if err := os.WriteFile(secret, []byte("hunter2\n"), 0600); err != nil {
		t.Fatalf("write tmp secret: %v", err)
	}
	got, err := substituteString("${file:" + secret + "}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want %q", got, "hunter2")
	}
}

func TestSubstitute_FileMissingNoDefault(t *testing.T) {
	path := "/nonexistent/path/to/secret"
	_, err := substituteString("${file:" + path + "}")
	if err == nil {
		t.Fatal("expected error for missing file with no default")
	}
	if !contains(err.Error(), path) {
		t.Errorf("error %q should mention the missing path %q", err.Error(), path)
	}
}

func TestSubstitute_FileMissingDefault(t *testing.T) {
	got, err := substituteString("${file:/nonexistent/path/secret:-fallback}")
	if err != nil {
		t.Fatalf("default should suppress missing-file error, got %v", err)
	}
	if got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

func TestSubstitute_DollarEscape(t *testing.T) {
	got, err := substituteString("price $$ 5 not ${env:X:-y}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "price $ 5 not y"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstitute_NestedStruct(t *testing.T) {
	t.Setenv("DEEP_VAL", "found")
	type Inner struct {
		Field string
	}
	type Outer struct {
		Top    string
		Nested Inner
		List   []string
		Map    map[string]interface{}
	}
	cfg := Outer{
		Top:    "${env:DEEP_VAL}",
		Nested: Inner{Field: "${env:DEEP_VAL}-deep"},
		List:   []string{"${env:DEEP_VAL}-list"},
		Map: map[string]interface{}{
			"key": "${env:DEEP_VAL}-map",
		},
	}
	if err := Substitute(&cfg); err != nil {
		t.Fatalf("Substitute returned error: %v", err)
	}
	if cfg.Top != "found" {
		t.Errorf("top: got %q", cfg.Top)
	}
	if cfg.Nested.Field != "found-deep" {
		t.Errorf("nested: got %q", cfg.Nested.Field)
	}
	if cfg.List[0] != "found-list" {
		t.Errorf("list: got %q", cfg.List[0])
	}
	if cfg.Map["key"] != "found-map" {
		t.Errorf("map: got %v", cfg.Map["key"])
	}
}

func TestSubstitute_PointerRequired(t *testing.T) {
	type S struct{ V string }
	err := Substitute(S{V: "${env:NOPE:-x}"})
	if err == nil {
		t.Fatal("expected error when called with non-pointer")
	}
}

func TestSubstitute_NilSafe(t *testing.T) {
	if err := Substitute(nil); err != nil {
		t.Errorf("nil input should be a no-op, got %v", err)
	}
}

func TestSubstitute_KeysNotMutated(t *testing.T) {
	// Spec rule 2: substitution applies to VALUES only, never to keys.
	t.Setenv("DO_NOT_USE", "if-this-shows-up-the-walker-is-wrong")
	m := map[string]interface{}{
		"${env:DO_NOT_USE}": "value",
	}
	if err := Substitute(&m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := m["${env:DO_NOT_USE}"]; !ok {
		t.Errorf("key was mutated; map keys must NOT be substituted")
	}
}

// contains is a tiny strings.Contains alias kept local so the test
// file doesn't pull in the strings package just for one call.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
