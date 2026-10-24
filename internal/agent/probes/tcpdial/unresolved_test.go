package tcpdial

import (
	"context"
	"net"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
)

func pointsNamed(points []data_store.DataPoint, name, target string) []data_store.DataPoint {
	var out []data_store.DataPoint
	for _, dp := range points {
		if v, _ := tagValue(dp.Tags, "target"); dp.Name == name && v == target {
			out = append(out, dp)
		}
	}
	return out
}

func TestUnresolvedLocalTarget(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	listener := entity.ListenerEndpoint("host-1", 5432, "tcp")

	cases := []struct {
		name         string
		target       string
		err          error
		wantEndpoint bool
		wantDiag     bool
	}{
		{"local refused", "127.0.0.1:5432", refused, true, true},
		{"local answering", "127.0.0.1:5432", nil, true, false},
		{"remote refused", "192.0.2.10:5432", refused, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newEntityTestProbe(t, "agent-1", tc.target)
			p.dial = func(_ context.Context, target string) dialResult {
				return dialResult{target: target, up: tc.err == nil, err: tc.err}
			}
			points, err := p.Collect()
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			status := findDatapoint(t, points, dbcommon.CheckResultMetric, tc.target)
			ep, has := tagValue(status.Tags, "service.endpoint")
			if has != tc.wantEndpoint || (has && ep != listener) {
				t.Errorf("check result service.endpoint = %q (present %v), want present=%v value %q", ep, has, tc.wantEndpoint, listener)
			}

			diag := pointsNamed(points, dbcommon.UnresolvedTargetMetric, tc.target)
			if (len(diag) == 1) != tc.wantDiag {
				t.Fatalf("diagnostic series = %d, want %v", len(diag), tc.wantDiag)
			}
			if tc.wantDiag {
				if r, _ := tagValue(diag[0].Tags, "reason"); r != dbcommon.UnresolvedReasonNoListener {
					t.Errorf("reason = %q, want %q", r, dbcommon.UnresolvedReasonNoListener)
				}
				if v, _ := tagValue(diag[0].Tags, "service.endpoint"); v != listener {
					t.Errorf("diagnostic service.endpoint = %q, want %q", v, listener)
				}
			}
		})
	}
}
