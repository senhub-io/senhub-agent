package app

import "testing"

func TestParseServiceEnvironmentBlock(t *testing.T) {
	got := parseServiceEnvironmentBlock([]string{"OTLP_BEARER_TOKEN=abc=def", "EMPTY=", "=nokey", "garbage"})
	if got["OTLP_BEARER_TOKEN"] != "abc=def" {
		t.Errorf("a value keeps its own '=' signs: %v", got)
	}
	if v, ok := got["EMPTY"]; !ok || v != "" {
		t.Errorf("an empty value is still set: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("entries without a key are skipped: %v", got)
	}
}

func TestImagePathConfig(t *testing.T) {
	cases := map[string]string{
		`"C:\Program Files\SenHub Agent\senhub-agent.exe" --config-path "C:\ProgramData\SenHub\agent.yaml"`: `C:\ProgramData\SenHub\agent.yaml`,
		`"C:\Program Files\SenHub Agent\senhub-agent.exe" --config-path=C:\SenHub\agent.yaml`:               `C:\SenHub\agent.yaml`,
		`"C:\Program Files\SenHub Agent\senhub-agent.exe"`:                                                  "",
	}
	for in, want := range cases {
		if got := imagePathConfig(in); got != want {
			t.Errorf("imagePathConfig(%s) = %q, want %q", in, got, want)
		}
	}
}
