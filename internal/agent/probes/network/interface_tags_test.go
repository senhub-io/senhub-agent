package network

import "testing"

func TestFirstIP(t *testing.T) {
	if got := firstIP(nil); got != "" {
		t.Fatalf("firstIP(nil) = %q, want empty", got)
	}
	if got := firstIP([]string{"192.0.2.1", "198.51.100.1"}); got != "192.0.2.1" {
		t.Fatalf("firstIP(...) = %q, want 192.0.2.1", got)
	}
}

func TestPrimaryWindowsIP(t *testing.T) {
	if got := primaryWindowsIP("192.0.2.1", "2001:db8::1"); got != "192.0.2.1" {
		t.Fatalf("primaryWindowsIP(ipv4, ipv6) = %q, want ipv4", got)
	}
	if got := primaryWindowsIP("", "2001:db8::1"); got != "2001:db8::1" {
		t.Fatalf("primaryWindowsIP(empty, ipv6) = %q, want ipv6", got)
	}
	if got := primaryWindowsIP("", ""); got != "" {
		t.Fatalf("primaryWindowsIP(empty, empty) = %q, want empty", got)
	}
}

func TestAppendIPLabel(t *testing.T) {
	got := appendIPLabel(nil, false, "192.0.2.1")
	if len(got) != 0 {
		t.Fatalf("appendIPLabel with include=false returned %d tags, want 0", len(got))
	}

	got = appendIPLabel(nil, true, "192.0.2.1")
	if len(got) != 1 {
		t.Fatalf("appendIPLabel with include=true returned %d tags, want 1", len(got))
	}
	if got[0].Key != "ip" || got[0].Value != "192.0.2.1" || got[0].Private {
		t.Fatalf("appendIPLabel tag = %+v, want ip=192.0.2.1 public", got[0])
	}

	got = appendIPLabel(nil, true, "")
	if len(got) != 0 {
		t.Fatalf("appendIPLabel with empty address returned %d tags, want 0", len(got))
	}
}
