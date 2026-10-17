package httpcheck

import (
	"net"
	"syscall"
	"testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/entity"
)

func TestUnresolvedLocalHTTPTarget(t *testing.T) {
	const local, remote = "http://127.0.0.1:8080/health", "http://192.0.2.10:8080/health"
	p := newTestProbe(t, map[string]interface{}{"targets": []interface{}{local, remote}})
	p.entitySrc.hostID = "host-1"
	p.entitySrc.agentID = func() string { return "agent-1" }
	p.check = func(target string) httpResult {
		return httpResult{target: target, err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}}
	}
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	listener := entity.ListenerEndpoint("host-1", 8080, "tcp")
	var localDiag, remoteDiag int
	for _, dp := range points {
		isLocal := hasTag(dp.Tags, "target", local)
		if dp.Name == dbcommon.CheckResultMetric && isLocal && !hasTag(dp.Tags, "service.endpoint", listener) {
			t.Errorf("local check result lacks service.endpoint=%s: %v", listener, dp.Tags)
		}
		if dp.Name != dbcommon.UnresolvedTargetMetric {
			continue
		}
		if isLocal {
			localDiag++
			if !hasTag(dp.Tags, "reason", dbcommon.UnresolvedReasonNoListener) || !hasTag(dp.Tags, "service.endpoint", listener) {
				t.Errorf("local diagnostic tags = %v", dp.Tags)
			}
		} else {
			remoteDiag++
		}
	}
	if localDiag != 1 || remoteDiag != 0 {
		t.Errorf("diagnostic series: local %d remote %d, want 1 and 0", localDiag, remoteDiag)
	}
}
