package ebpfhelper

import (
	"errors"
	"strings"
	"testing"
)

const bpfAndPerfmon = uint64(1<<39 | 1<<38)

func host(release string, btf bool, caps uint64) Host {
	return Host{
		KernelRelease: func() (string, error) { return release, nil },
		HasBTF:        func() bool { return btf },
		EffectiveCaps: func() (uint64, error) { return caps, nil },
	}
}

func refusal(t *testing.T, h Host) *RefusalError {
	t.Helper()
	_, err := Preflight(h)
	var ref *RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("want a refusal, got %v", err)
	}
	return ref
}

func TestPreflightAccepts(t *testing.T) {
	for _, rel := range []string{"5.8.0", "6.8.0-142-generic", "5.14.0-427.el9.x86_64"} {
		if _, err := Preflight(host(rel, true, bpfAndPerfmon)); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	// Capabilities beyond the two are not a reason to refuse.
	if _, err := Preflight(host("6.8.0", true, bpfAndPerfmon|1<<21)); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightRefusesOldKernel(t *testing.T) {
	for _, rel := range []string{"4.18.0-553.el8_10.x86_64", "5.4.0-100-generic", "5.7.19"} {
		ref := refusal(t, host(rel, true, bpfAndPerfmon))
		if ref.Code != ExitUnsupported || !strings.Contains(ref.Reason, "unsupported") || !strings.Contains(ref.Reason, "RHEL 8") {
			t.Fatalf("%s: %+v", rel, ref)
		}
	}
}

func TestPreflightRefusesWithoutBTF(t *testing.T) {
	ref := refusal(t, host("6.8.0", false, bpfAndPerfmon))
	if ref.Code != ExitUnsupported || !strings.Contains(ref.Reason, "BTF") {
		t.Fatalf("%+v", ref)
	}
}

func TestPreflightRefusesMissingCapabilities(t *testing.T) {
	ref := refusal(t, host("6.8.0", true, 0))
	if ref.Code != ExitNoCaps || !strings.Contains(ref.Reason, "CAP_BPF") || !strings.Contains(ref.Reason, "CAP_PERFMON") {
		t.Fatalf("%+v", ref)
	}
	ref = refusal(t, host("6.8.0", true, 1<<39))
	if !strings.HasPrefix(ref.Reason, "missing capabilities CAP_PERFMON:") {
		t.Fatalf("only the missing capability must be named: %+v", ref)
	}
}

func TestPreflightErrors(t *testing.T) {
	h := host("6.8.0", true, bpfAndPerfmon)
	h.KernelRelease = func() (string, error) { return "", errors.New("boom") }
	if ref := refusal(t, h); ref.Code != ExitFailed {
		t.Fatalf("%+v", ref)
	}
	if ref := refusal(t, host("garbage", true, bpfAndPerfmon)); ref.Code != ExitFailed {
		t.Fatalf("%+v", ref)
	}
	h = host("6.8.0", true, 0)
	h.EffectiveCaps = func() (uint64, error) { return 0, errors.New("no proc") }
	if ref := refusal(t, h); ref.Code != ExitFailed {
		t.Fatalf("%+v", ref)
	}
}

func TestParseKernel(t *testing.T) {
	cases := map[string][2]int{
		"6.8.0-142-generic": {6, 8}, "5.15.0": {5, 15}, "4.18.0-553.el8": {4, 18}, "6.1": {6, 1}, "6.12-rc1": {6, 12},
	}
	for in, want := range cases {
		maj, min, err := ParseKernel(in)
		if err != nil || maj != want[0] || min != want[1] {
			t.Fatalf("%s: %d.%d %v", in, maj, min, err)
		}
	}
	for _, bad := range []string{"", "6", "x.y", "6.x"} {
		if _, _, err := ParseKernel(bad); err == nil {
			t.Fatalf("%q must not parse", bad)
		}
	}
}

func TestParseCapEff(t *testing.T) {
	status := "Name:\tsenhub-ebpf\nCapInh:\t0000000000000000\nCapEff:\t000000c000000000\nCapBnd:\t000000c000000000\n"
	caps, err := ParseCapEff(status)
	if err != nil || caps != bpfAndPerfmon {
		t.Fatalf("%x %v", caps, err)
	}
	if _, err := ParseCapEff("Name:\tx\n"); err == nil {
		t.Fatal("a status without CapEff must fail")
	}
}
