package network

import (
	"errors"
	"senhub-agent.go/internal/agent/services/netconn"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func TestHostTCPDataPoints_Connections(t *testing.T) {
	base := []tags.Tag{{Key: "host", Value: "h1"}}
	src := tcpSource{
		conns: func() ([]netconn.Conn, error) {
			return []netconn.Conn{
				{State: netconn.StateEstablished},
				{State: netconn.StateListen},
				{State: netconn.StateEstablished},
				{State: 6},
			}, nil
		},
	}
	ts := time.Now()
	points, errs := hostTCPDataPoints(src, base, ts)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	var connPoints []data_store.DataPoint
	for _, p := range points {
		if p.Name == "tcp_connections" {
			connPoints = append(connPoints, p)
		}
	}
	if len(connPoints) != len(tcpStateNames) {
		t.Fatalf("expected %d connection state points, got %d", len(tcpStateNames), len(connPoints))
	}
	got := map[string]float64{}
	for _, p := range connPoints {
		for _, tg := range p.Tags {
			if tg.Key == "state" {
				got[tg.Value] = p.Value
			}
		}
	}
	for state, want := range map[string]float64{"established": 2, "listen": 1, "time_wait": 1, "close_wait": 0} {
		if got[state] != want {
			t.Errorf("state %s = %v, want %v", state, got[state], want)
		}
	}
}

func TestHostTCPDataPoints_Counters(t *testing.T) {
	base := []tags.Tag{{Key: "host", Value: "h1"}}
	src := tcpSource{
		stats: func() (TCPStats, error) {
			return TCPStats{
				ActiveOpens:        10,
				PassiveOpens:       20,
				AttemptFails:       3,
				EstabResets:        4,
				RetransSegs:        5,
				OutRsts:            6,
				ListenOverflows:    7,
				HasListenOverflows: true,
			}, nil
		},
	}
	ts := time.Now()
	points, errs := hostTCPDataPoints(src, base, ts)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	expected := map[string]float64{
		"tcp_active_opens":     10,
		"tcp_passive_opens":    20,
		"tcp_attempt_fails":    3,
		"tcp_estab_resets":     4,
		"tcp_retrans_segs":     5,
		"tcp_out_rsts":         6,
		"tcp_listen_overflows": 7,
	}
	for _, p := range points {
		if want, ok := expected[p.Name]; ok {
			if p.Value != want {
				t.Errorf("metric %s = %v, want %v", p.Name, p.Value, want)
			}
			if len(p.Tags) != len(base) {
				t.Errorf("metric %s tags length = %d, want %d", p.Name, len(p.Tags), len(base))
			}
		}
	}
	if len(points) != len(expected) {
		t.Errorf("got %d points, want %d", len(points), len(expected))
	}
}

func TestHostTCPDataPoints_NoListenOverflows(t *testing.T) {
	src := tcpSource{
		stats: func() (TCPStats, error) {
			return TCPStats{
				ActiveOpens:        1,
				HasListenOverflows: false,
			}, nil
		},
	}
	ts := time.Now()
	points, errs := hostTCPDataPoints(src, nil, ts)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	for _, p := range points {
		if p.Name == "tcp_listen_overflows" {
			t.Fatal("should not have tcp_listen_overflows when HasListenOverflows is false")
		}
	}
}

func TestHostTCPDataPoints_ConnErrorDoesNotPreventCounters(t *testing.T) {
	base := []tags.Tag{{Key: "host", Value: "h1"}}
	src := tcpSource{
		conns: func() ([]netconn.Conn, error) {
			return nil, errors.New("conn error")
		},
		stats: func() (TCPStats, error) {
			return TCPStats{
				ActiveOpens:        10,
				HasListenOverflows: false,
			}, nil
		},
	}
	ts := time.Now()
	points, errs := hostTCPDataPoints(src, base, ts)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	foundCounter := false
	for _, p := range points {
		if p.Name == "tcp_active_opens" {
			foundCounter = true
			break
		}
	}
	if !foundCounter {
		t.Error("expected tcp_active_opens counter even with conn error")
	}
}

func TestHostTCPDataPoints_EmptySource(t *testing.T) {
	ts := time.Now()
	points, errs := hostTCPDataPoints(tcpSource{}, nil, ts)
	if len(points) != 0 || len(errs) != 0 {
		t.Fatalf("expected zero points and errors, got %d points, %d errors", len(points), len(errs))
	}
}

func TestHostTCPDataPoints_BaseNotModified(t *testing.T) {
	base := []tags.Tag{{Key: "host", Value: "h1"}}
	originalLen := len(base)
	src := tcpSource{
		conns: func() ([]netconn.Conn, error) {
			return []netconn.Conn{{State: netconn.StateEstablished}}, nil
		},
		stats: func() (TCPStats, error) {
			return TCPStats{ActiveOpens: 1}, nil
		},
	}
	ts := time.Now()
	_, _ = hostTCPDataPoints(src, base, ts)
	if len(base) != originalLen {
		t.Fatalf("base slice length changed: %d -> %d", originalLen, len(base))
	}
	if base[0].Key != "host" || base[0].Value != "h1" {
		t.Fatalf("base slice modified: %+v", base)
	}
}
