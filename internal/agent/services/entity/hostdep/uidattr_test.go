package hostdep

import (
	"testing"

	gnet "github.com/shirou/gopsutil/v3/net"
)

func TestUnivoqueNames(t *testing.T) {
	procs := []procOwner{
		{UID: 1000, Name: "postgres"},
		{UID: 1000, Name: "postgres"},
		{UID: 33, Name: "nginx"},
		{UID: 33, Name: "php-fpm"},
		{UID: 0, Name: ""},
		{UID: 0, Name: "sshd"},
	}
	got := univoqueNames(procs)
	if got[1000] != "postgres" {
		t.Fatalf("uid 1000 = %q, want postgres", got[1000])
	}
	if _, ok := got[33]; ok {
		t.Fatalf("uid 33 present, want absent")
	}
	if _, ok := got[0]; ok {
		t.Fatalf("uid 0 present, want absent")
	}
}

func TestUIDViewNameFor(t *testing.T) {
	v := &uidView{
		uidByTuple: map[tupleKey]uint32{
			{"10.0.0.5", 1234, "8.8.8.8", 53}: 1000,
			{"10.0.0.6", 1234, "8.8.8.8", 53}: 33,
		},
		nameByUID: map[uint32]string{
			1000: "postgres",
		},
	}

	if n, ok := v.nameFor("10.0.0.5", 1234, "8.8.8.8", 53); !ok || n != "postgres" {
		t.Fatalf("unambiguous = %q, %v", n, ok)
	}
	if _, ok := v.nameFor("10.0.0.6", 1234, "8.8.8.8", 53); ok {
		t.Fatalf("ambiguous uid reported true")
	}
	if _, ok := v.nameFor("10.0.0.7", 1234, "8.8.8.8", 53); ok {
		t.Fatalf("unknown tuple reported true")
	}
	var nilv *uidView
	if _, ok := nilv.nameFor("10.0.0.5", 1234, "8.8.8.8", 53); ok {
		t.Fatalf("nil view reported true")
	}
}

func TestNormalizeIP(t *testing.T) {
	if got := normalizeIP("::ffff:10.0.0.5"); got != "10.0.0.5" {
		t.Fatalf("normalizeIP = %q", got)
	}
}

func TestScrapeUIDViewUnambiguous(t *testing.T) {
	s := New(func() string { return "host-1" }, 1, nil)
	s.procName = func(int32) string { return "" }
	s.agentID = func() string { return "" }
	s.selfPID = func() int32 { return -1 }
	s.ephemeral = func() (uint32, uint32) { return 60000, 61000 }
	s.procCreated = func(int32) (int64, bool) { return 0, false }

	view := &uidView{
		uidByTuple: map[tupleKey]uint32{
			{"10.0.0.5", 50000, "93.184.216.34", 443}: 1000,
		},
		nameByUID: map[uint32]string{1000: "postgres"},
	}
	conns := []gnet.ConnectionStat{
		{
			Status: "ESTABLISHED",
			Laddr:  gnet.Addr{IP: "10.0.0.5", Port: 50000},
			Raddr:  gnet.Addr{IP: "93.184.216.34", Port: 443},
			Pid:    0,
		},
	}
	deps, _, unattributable := s.scrape(conns, "host-1", nil, view)
	if unattributable != 0 {
		t.Fatalf("unattributable = %d, want 0", unattributable)
	}
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].svcID != "postgres@host-1" || deps[0].svcName != "postgres" {
		t.Fatalf("deps[0] = %+v, want postgres@host-1/postgres", deps[0])
	}
}

func TestScrapeUIDViewAmbiguous(t *testing.T) {
	s := New(func() string { return "host-1" }, 1, nil)
	s.procName = func(int32) string { return "" }
	s.agentID = func() string { return "" }
	s.selfPID = func() int32 { return -1 }
	s.ephemeral = func() (uint32, uint32) { return 60000, 61000 }
	s.procCreated = func(int32) (int64, bool) { return 0, false }

	view := &uidView{
		uidByTuple: map[tupleKey]uint32{
			{"10.0.0.6", 50001, "93.184.216.34", 443}: 33,
		},
		nameByUID: map[uint32]string{},
	}
	conns := []gnet.ConnectionStat{
		{
			Status: "ESTABLISHED",
			Laddr:  gnet.Addr{IP: "10.0.0.6", Port: 50001},
			Raddr:  gnet.Addr{IP: "93.184.216.34", Port: 443},
			Pid:    0,
		},
	}
	deps, _, unattributable := s.scrape(conns, "host-1", nil, view)
	if unattributable != 1 {
		t.Fatalf("unattributable = %d, want 1", unattributable)
	}
	if len(deps) != 0 {
		t.Fatalf("len(deps) = %d, want 0", len(deps))
	}
}
