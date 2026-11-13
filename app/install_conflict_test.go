package app

import (
	"strings"
	"testing"
)

func TestExistingServiceConflict(t *testing.T) {
	const zipExe = `C:\lab\senhub-agent.exe`
	const msiImagePath = `"C:\Program Files\SenHub Agent\senhub-agent.exe" run --config-path C:\ProgramData\SenHub\agent.yaml`
	for _, tc := range []struct {
		name      string
		imagePath string
		thisExe   string
		msi       bool
		want      []string
	}{
		{name: "no service", imagePath: "", thisExe: zipExe},
		{name: "re-run from the registered exe", imagePath: `"C:\lab\senhub-agent.exe" run --config-path C:\ProgramData\SenHub\agent.yaml`, thisExe: zipExe},
		{name: "re-run, other case", imagePath: `"c:\LAB\Senhub-Agent.exe" run`, thisExe: zipExe},
		{name: "MSI service", imagePath: msiImagePath, thisExe: zipExe, msi: true,
			want: []string{"installed by the MSI", `C:\Program Files\SenHub Agent\senhub-agent.exe`, "Nothing was changed", "msiexec /i"}},
		{name: "service from another ZIP", imagePath: `"D:\old\senhub-agent.exe" run`, thisExe: zipExe,
			want: []string{`running D:\old\senhub-agent.exe`, "Nothing was changed", `"D:\old\senhub-agent.exe" update <version>`, "deletes its configuration"}},
		{name: "unquoted legacy ImagePath", imagePath: `C:\SenHub\senhub-agent.exe --config-path C:\SenHub\agent.yaml`, thisExe: zipExe,
			want: []string{`running C:\SenHub\senhub-agent.exe`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := existingServiceConflict(tc.imagePath, tc.thisExe, tc.msi)
			if len(tc.want) == 0 {
				if got != "" {
					t.Fatalf("want install to go on, got refusal:\n%s", got)
				}
				return
			}
			if got == "" {
				t.Fatal("want a refusal, got none")
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("refusal lacks %q:\n%s", w, got)
				}
			}
		})
	}
}
