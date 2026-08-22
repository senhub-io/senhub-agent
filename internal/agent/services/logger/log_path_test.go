package logger

import (
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
)

// TestDefaultInstallKeepsTheHistoricFileName is the constraint that
// bounds this fix. Every log collector, logrotate rule and support
// runbook in the field names senhubagent.log; renaming the fleet's log
// file to solve a second-instance problem would be a worse trade than
// the problem.
func TestDefaultInstallKeepsTheHistoricFileName(t *testing.T) {
	installed, err := cliArgs.GetAbsoluteConfigPath("")
	if err != nil {
		t.Skipf("no installed config path on this platform: %v", err)
	}

	cases := []*cliArgs.ParsedArgs{
		nil,
		{},
		{ConfigPath: ""},
		{ConfigPath: "   "},
		{ConfigPath: installed},
	}
	for _, args := range cases {
		if got := logFileNameFor(args); got != "senhubagent.log" {
			t.Errorf("logFileNameFor(%+v) = %q, want senhubagent.log", args, got)
		}
	}
}

// TestSecondInstanceGetsItsOwnFile is the defect. Two agents sharing one
// file is destructive rather than untidy: each carries its own rotator,
// and lumberjack rotation opens the target with O_TRUNC, so whichever
// rotates first destroys the other's history (#838).
func TestSecondInstanceGetsItsOwnFile(t *testing.T) {
	lab := logFileNameFor(&cliArgs.ParsedArgs{ConfigPath: "/opt/senhub-lab/conf/agent.yaml"})
	if lab == "senhubagent.log" {
		t.Fatal("a second instance still writes the default file — it would truncate the service's log")
	}

	other := logFileNameFor(&cliArgs.ParsedArgs{ConfigPath: "/opt/another/agent.yaml"})
	if other == lab {
		t.Errorf("two distinct instances share %q", lab)
	}
}

// TestNameIsStableAcrossRuns: the same instance must land in the same
// file every time, or restarting it would scatter its history across
// files nobody can find.
func TestNameIsStableAcrossRuns(t *testing.T) {
	const p = "/opt/senhub-lab/conf/agent.yaml"
	first := logFileNameFor(&cliArgs.ParsedArgs{ConfigPath: p})
	for i := 0; i < 5; i++ {
		if got := logFileNameFor(&cliArgs.ParsedArgs{ConfigPath: p}); got != first {
			t.Fatalf("run %d produced %q, want %q", i, got, first)
		}
	}
}

// TestNameDoesNotLeakThePath keeps a log directory listing from
// disclosing where someone's configuration lives.
func TestNameDoesNotLeakThePath(t *testing.T) {
	got := logFileNameFor(&cliArgs.ParsedArgs{ConfigPath: "/home/alice/secret-project/agent.yaml"})
	for _, leak := range []string{"alice", "secret-project", "home"} {
		if contains(got, leak) {
			t.Errorf("file name %q leaks %q from the config path", got, leak)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
