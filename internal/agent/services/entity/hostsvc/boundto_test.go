package hostsvc

import (
	"testing"

	"senhub-agent.go/internal/agent/services/entity"
)

func TestBuildObservation_BoundToAddress(t *testing.T) {
	ls := []listener{
		{Pid: 10, Proc: "pg", Address: "10.0.0.5", Port: 5432, Transport: "tcp"},
		{Pid: 20, Proc: "nginx", Address: "0.0.0.0", Port: 80, Transport: "tcp"},
		{Pid: 30, Proc: "redis", Address: "127.0.0.1", Port: 6379, Transport: "tcp"},
		{Pid: 40, Proc: "web", Address: "172.17.0.3", Port: 8080, Transport: "tcp"},
	}
	ipToIface := map[string]string{"10.0.0.5": "eth0", "127.0.0.1": "lo", "172.17.0.3": "docker0"}
	obs := buildObservation("h-1", ls, ipToIface, 0)

	var bound []entity.Relation
	for _, r := range obs.Relations {
		if r.Type == relBoundTo {
			bound = append(bound, r)
		}
	}
	if len(bound) != 1 {
		t.Fatalf("want exactly 1 bound_to (routable bind only), got %+v", bound)
	}
	r := bound[0]
	if r.FromType != entityTypeServiceListener || r.FromID[idKeyServiceEndpoint] != "h-1:5432/tcp" ||
		r.ToType != entityTypeNetworkAddress || r.ToID[idKeyNetworkAddress] != "10.0.0.5" {
		t.Errorf("bound_to must link the listener to network.address{10.0.0.5}: %+v", r)
	}
}
