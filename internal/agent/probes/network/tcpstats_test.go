package network

import (
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/services/netconn"
)

func TestParseProcNetSNMP(t *testing.T) {
	input := `Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts InCsumErrors
Tcp: 1 200 120000 -1 1500 320 12 7 25 900000 850000 4321 0 88 0`
	stats, err := parseProcNetSNMP(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ActiveOpens != 1500 {
		t.Errorf("ActiveOpens = %d, want 1500", stats.ActiveOpens)
	}
	if stats.PassiveOpens != 320 {
		t.Errorf("PassiveOpens = %d, want 320", stats.PassiveOpens)
	}
	if stats.AttemptFails != 12 {
		t.Errorf("AttemptFails = %d, want 12", stats.AttemptFails)
	}
	if stats.EstabResets != 7 {
		t.Errorf("EstabResets = %d, want 7", stats.EstabResets)
	}
	if stats.RetransSegs != 4321 {
		t.Errorf("RetransSegs = %d, want 4321", stats.RetransSegs)
	}
	if stats.OutRsts != 88 {
		t.Errorf("OutRsts = %d, want 88", stats.OutRsts)
	}
}

func TestParseProcNetNetstat(t *testing.T) {
	input := `TcpExt: SyncookiesSent SyncookiesRecv ListenOverflows ListenDrops
TcpExt: 1 2 3 4`
	var stats TCPStats
	err := parseProcNetNetstat(strings.NewReader(input), &stats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ListenOverflows != 3 {
		t.Errorf("ListenOverflows = %d, want 3", stats.ListenOverflows)
	}
	if !stats.HasListenOverflows {
		t.Error("HasListenOverflows = false, want true")
	}
}

func TestParseProcNetSNMPErrors(t *testing.T) {
	// Missing Tcp: lines
	_, err := parseProcNetSNMP(strings.NewReader("some other content"))
	if err == nil {
		t.Error("expected error for missing Tcp: lines, got nil")
	}
	// Column count mismatch
	input := `Tcp: A B
Tcp: 1`
	_, err = parseProcNetSNMP(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for mismatched columns, got nil")
	}
}

func TestParseProcNetNetstatNoTcpExt(t *testing.T) {
	var stats TCPStats
	err := parseProcNetNetstat(strings.NewReader("some other content"), &stats)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.HasListenOverflows {
		t.Error("HasListenOverflows should be false when TcpExt absent")
	}
}

func TestCountByState(t *testing.T) {
	conns := []netconn.Conn{
		{State: 1},
		{State: 1},
		{State: 10},
		{State: 99},
	}
	counts := countByState(conns)
	expected := map[string]int{
		"established": 2,
		"listen":      1,
	}
	for name, want := range expected {
		if got := counts[name]; got != want {
			t.Errorf("counts[%q] = %d, want %d", name, got, want)
		}
	}
	// Verify all known states are present, even if zero.
	if len(counts) != len(tcpStateNames) {
		t.Errorf("counts map has %d entries, want %d", len(counts), len(tcpStateNames))
	}
	for _, name := range tcpStateNames {
		if _, ok := counts[name]; !ok {
			t.Errorf("state %q missing from counts", name)
		}
	}
	if counts["time_wait"] != 0 {
		t.Errorf("time_wait count = %d, want 0", counts["time_wait"])
	}
}
