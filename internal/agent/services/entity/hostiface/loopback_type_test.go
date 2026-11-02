package hostiface

import "testing"

func TestALoopbackFlagGivesTheLoopbackType(t *testing.T) {
	if got := resolveLinkMeta("lo-test-not-in-sysfs", []string{"up", "loopback"}).Type; got != "loopback" {
		t.Fatalf("type = %q for a loopback flag, want loopback", got)
	}
	if got := resolveLinkMeta("lo", []string{"up"}).Type; got == "loopback" {
		t.Fatal("the name lo alone gave loopback: the type must come from the flag")
	}
}
