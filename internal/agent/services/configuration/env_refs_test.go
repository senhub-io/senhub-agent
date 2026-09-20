package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnsetEnvReferences_NamesTheVariablesNotSetInThisProcess(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(main, []byte("agent:\n  key: ${env:SENHUB_TEST_KEY_UNSET}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "strategies.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	frag := filepath.Join(dir, "strategies.d", "otlp.yaml")
	body := "headers:\n  Authorization: \"Bearer ${env:SENHUB_TEST_TOKEN_UNSET}\"\n" +
		"  X-Set: ${env:SENHUB_TEST_SET}\n" +
		"  X-Default: ${env:SENHUB_TEST_DEFAULTED:-x}\n" +
		"  X-Twice: ${env:SENHUB_TEST_TOKEN_UNSET}\n"
	if err := os.WriteFile(frag, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SENHUB_TEST_SET", "")

	got := UnsetEnvReferences(main)
	want := []EnvReference{
		{Name: "SENHUB_TEST_KEY_UNSET", File: main},
		{Name: "SENHUB_TEST_TOKEN_UNSET", File: frag},
	}
	if len(got) != len(want) {
		t.Fatalf("refs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("refs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestUnsetEnvReferences_IgnoresAnUnreadableFile(t *testing.T) {
	if got := UnsetEnvReferences(filepath.Join(t.TempDir(), "missing.yaml")); len(got) != 0 {
		t.Errorf("refs = %+v, want none", got)
	}
}
