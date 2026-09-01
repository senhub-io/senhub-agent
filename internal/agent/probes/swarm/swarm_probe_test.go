package swarm

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/dockerdial"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

// newTestProbe wires the probe to a fake Engine served over a Unix socket, the
// same transport production uses — an httptest TCP server would exercise a code
// path the probe never takes.
func newTestProbe(t *testing.T, handler http.Handler) *swarmProbe {
	t.Helper()

	sock, err := shortSocketPath(t)
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		// The probe dials a Unix socket, which is how the Docker Engine is
		// reached on Linux and macOS. Windows exposes the Engine over a named
		// pipe instead, so a platform without AF_UNIX is not a broken test —
		// it is a platform this probe's transport does not serve. Skipping is
		// stated with its reason rather than hidden, and only here: on every
		// platform that does support it, a listen failure is a hard error.
		if runtime.GOOS == "windows" {
			t.Skipf("Unix sockets unavailable on this runner (%v); the swarm probe reaches Docker over a Unix socket, not a named pipe", err)
		}
		t.Fatalf("listen unix on %s: %v", sock, err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: handler}}
	srv.Start()
	t.Cleanup(srv.Close)

	p, err := NewSwarmProbe(map[string]interface{}{"socket_path": sock},
		logger.NewLogger(&cliArgs.ParsedArgs{Env: "test"}))
	if err != nil {
		t.Fatalf("NewSwarmProbe: %v", err)
	}
	return p.(*swarmProbe)
}

// shortSocketPath returns a temp socket path under the platform's socket-name
// limit, roughly 104 bytes on macOS and BSD and 108 on Linux.
//
// NOT t.TempDir(): it embeds the test name in the path, which pushed every
// test in this file over the macOS limit and made them all skip — a skipped
// test guards nothing while looking exactly like a passing one.
//
// The default temp root is tried first because it is the portable answer, and
// "/tmp" only as a fallback for the platforms whose default root is long. A
// hardcoded "/tmp" is not portable: it does not exist on Windows, which is how
// this file went red in CI after being fixed for macOS.
func shortSocketPath(t *testing.T) (string, error) {
	t.Helper()
	for _, root := range []string{"", "/tmp"} {
		if root == "/tmp" {
			if _, err := os.Stat(root); err != nil {
				continue
			}
		}
		dir, err := os.MkdirTemp(root, "sw")
		if err != nil {
			continue
		}
		sock := filepath.Join(dir, "d.sock")
		if len(sock) <= 100 {
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			return sock, nil
		}
		_ = os.RemoveAll(dir)
	}
	return "", fmt.Errorf("no temp root produced a socket path under the platform limit")
}

// engine serves canned JSON per path; an absent path answers 404 so a test that
// forgets one fails loudly instead of silently reading an empty list.
func engine(t *testing.T, routes map[string]any) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for path, body := range routes {
			if strings.HasSuffix(r.URL.Path, path) {
				if s, ok := body.(errBody); ok {
					w.WriteHeader(s.code)
					_, _ = w.Write([]byte(s.text))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(body)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

type errBody struct {
	code int
	text string
}

func value(t *testing.T, points []data_store.DataPoint, name string, wantTags map[string]string) (float64, bool) {
	t.Helper()
outer:
	for _, p := range points {
		if p.Name != name {
			continue
		}
		for k, v := range wantTags {
			found := false
			for _, tag := range p.Tags {
				if tag.Key == k && tag.Value == v {
					found = true
					break
				}
			}
			if !found {
				continue outer
			}
		}
		return p.Value, true
	}
	return 0, false
}

// A worker answers 503 to every cluster query. Reporting that as an empty
// cluster — zero nodes, zero services, all healthy — is the worst possible
// answer, because nothing looks wrong.
func TestCollect_WorkerNodeIsNotAnEmptyCluster(t *testing.T) {
	p := newTestProbe(t, engine(t, map[string]any{
		"/swarm": errBody{http.StatusServiceUnavailable, "This node is not a swarm manager."},
	}))

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if up, _ := value(t, points, "senhub.swarm.up", nil); up != 0 {
		t.Errorf("senhub.swarm.up = %v on a worker, want 0", up)
	}
	if v, ok := value(t, points, "senhub.swarm.node_role_state", map[string]string{"state": "worker"}); !ok || v != 1 {
		t.Error("the worker state series must be 1 so the reason is readable, not just the absence of data")
	}
	if _, ok := value(t, points, "swarm.cluster.nodes", nil); ok {
		t.Error("a worker reported a node count; it cannot see one")
	}
}

// "Not in a swarm at all" and "not a manager" are both 503 and mean different
// things: one is a probe pointed at the wrong node, the other at a machine that
// was never clustered.
func TestCollect_NotInSwarmIsDistinctFromWorker(t *testing.T) {
	p := newTestProbe(t, engine(t, map[string]any{
		"/swarm": errBody{http.StatusServiceUnavailable, "This node is not part of a swarm"},
	}))
	points, _ := p.Collect()
	if v, ok := value(t, points, "senhub.swarm.node_role_state", map[string]string{"state": "not_in_swarm"}); !ok || v != 1 {
		t.Error("a non-clustered engine must report not_in_swarm, not worker")
	}
}

// Quorum is "strictly more than half". Two managers with one reachable is NOT
// quorum — the most common mistake, and the exact shape of a two-manager
// cluster that has just lost one.
func TestHasQuorum_BoundaryIsStrictMajority(t *testing.T) {
	cases := []struct {
		managers, reachable int
		want                bool
	}{
		{1, 1, true},
		{1, 0, false},
		{2, 1, false}, // the trap
		{2, 2, true},
		{3, 2, true},
		{3, 1, false},
		{5, 3, true},
		{5, 2, false},
		{0, 0, false},
	}
	for _, c := range cases {
		if got := hasQuorum(c.managers, c.reachable); got != c.want {
			t.Errorf("hasQuorum(%d, %d) = %v, want %v", c.managers, c.reachable, got, c.want)
		}
	}
}

// A global service declares no replica count. Reading the missing field as 0 —
// the obvious interpretation — makes every global service look permanently
// unconverged.
func TestServiceMode_GlobalDesiredComesFromTasks(t *testing.T) {
	s := &service{ID: "svc1"}
	s.Spec.Mode.Global = &struct{}{}
	tasks := []task{
		{ServiceID: "svc1", DesiredState: "running"},
		{ServiceID: "svc1", DesiredState: "running"},
		{ServiceID: "svc1", DesiredState: "shutdown"}, // replaced, not wanted
		{ServiceID: "other", DesiredState: "running"},
	}
	mode, desired := serviceMode(s, tasks)
	if mode != "global" {
		t.Errorf("mode = %q, want global", mode)
	}
	if desired != 2 {
		t.Errorf("desired = %d, want 2 (the tasks Swarm intends to run)", desired)
	}
}

// A service attaches to networks in either of two spec slots, by id or by name
// depending on how it was created. Reading one place or one spelling reports a
// service as attached to nothing — which reads exactly like correct isolation.
func TestServiceNetworkIDs_ResolvesBothSlotsAndBothSpellings(t *testing.T) {
	front := network{ID: "netA", Name: "frontend", Driver: "overlay", Scope: "swarm"}
	back := network{ID: "netB", Name: "backend", Driver: "overlay", Scope: "swarm"}
	byID := map[string]*network{"netA": &front, "netB": &back}
	byName := map[string]*network{"frontend": &front, "backend": &back}

	s := &service{ID: "s1"}
	s.Spec.TaskTemplate.Networks = []networkAttachmentConfig{{Target: "netA"}} // by id
	s.Spec.Networks = []networkAttachmentConfig{{Target: "backend"}}           // by name, legacy slot

	got := serviceNetworkIDs(s, byID, byName)
	if len(got) != 2 {
		t.Fatalf("resolved %v, want both netA and netB", got)
	}

	// The same network named twice must not be counted twice.
	s2 := &service{ID: "s2"}
	s2.Spec.TaskTemplate.Networks = []networkAttachmentConfig{{Target: "netA"}}
	s2.Spec.Networks = []networkAttachmentConfig{{Target: "frontend"}}
	if got := serviceNetworkIDs(s2, byID, byName); len(got) != 1 {
		t.Errorf("resolved %v, want netA once", got)
	}
}

// A service with a published port rides the ingress segment whether or not it
// declares it — that is how the routing mesh reaches it, and a reachability map
// that omits the edge is wrong about where the traffic enters.
func TestServiceNetworkIDs_PublishedPortImpliesIngress(t *testing.T) {
	ing := network{ID: "netIng", Name: "ingress", Driver: "overlay", Scope: "swarm", Ingress: true}
	byID := map[string]*network{"netIng": &ing}
	byName := map[string]*network{"ingress": &ing}

	s := &service{ID: "s1"}
	s.Endpoint.Ports = []portConfig{{PublishedPort: 8080, TargetPort: 80, Protocol: "tcp"}}

	if got := serviceNetworkIDs(s, byID, byName); len(got) != 1 || got[0] != "netIng" {
		t.Errorf("resolved %v, want the ingress segment", got)
	}
}

// A local overlay is not part of the cluster data plane. Counting it reports a
// segment no other node can see.
func TestOverlayNetworks_KeepsOnlySwarmScopedOverlays(t *testing.T) {
	in := []network{
		{ID: "1", Name: "app", Driver: "overlay", Scope: "swarm"},
		{ID: "2", Name: "bridge", Driver: "bridge", Scope: "local"},
		{ID: "3", Name: "local-overlay", Driver: "overlay", Scope: "local"},
	}
	got := overlayNetworks(in)
	if len(got) != 1 || got[0].ID != "1" {
		t.Errorf("kept %v, want only the swarm-scoped overlay", got)
	}
}

// An overlay running out of addresses refuses new tasks with an error naming
// neither the network nor the exhaustion.
func TestSubnetCapacity(t *testing.T) {
	cases := map[string]int64{
		"10.0.1.0/24":  254,
		"10.0.0.0/16":  65534,
		"10.0.0.0/30":  2,
		"":             0,
		"10.0.0.0":     0,
		"10.0.0.0/abc": 0,
		"fd00::/64":    0, // an IPv6 count no panel can render is not an answer
	}
	for cidr, want := range cases {
		if got := subnetCapacity(cidr); got != want {
			t.Errorf("subnetCapacity(%q) = %d, want %d", cidr, got, want)
		}
	}
}

// End to end on a small cluster: the numbers a reader would check first.
func TestCollect_ManagerReportsTheClusterItSees(t *testing.T) {
	replicas := int64(3)
	svc := service{ID: "svc1"}
	svc.Spec.Name = "api"
	svc.Spec.Mode.Replicated = &struct {
		Replicas *int64 `json:"Replicas"`
	}{Replicas: &replicas}
	svc.Spec.TaskTemplate.Networks = []networkAttachmentConfig{{Target: "netA"}}
	svc.Endpoint.VirtualIPs = []struct {
		NetworkID string `json:"NetworkID"`
		Addr      string `json:"Addr"`
	}{{NetworkID: "netA", Addr: "10.0.1.5/24"}}

	mgr := node{ID: "n1"}
	mgr.Description.Hostname = "mgr-1"
	mgr.Spec.Role = "manager"
	mgr.Spec.Availability = "active"
	mgr.Status.State = "ready"
	mgr.ManagerStatus = &struct {
		Leader       bool   `json:"Leader"`
		Reachability string `json:"Reachability"`
		Addr         string `json:"Addr"`
	}{Leader: true, Reachability: "reachable"}

	handler := engine(t, map[string]any{
		"/swarm":    map[string]any{"ID": "cluster-xyz", "Spec": map[string]any{"Name": "default"}},
		"/nodes":    []node{mgr},
		"/services": []service{svc},
		"/tasks": []task{
			{ID: "t1", ServiceID: "svc1", NodeID: "n1", DesiredState: "running", Status: struct {
				State           string `json:"State"`
				Message         string `json:"Message"`
				Err             string `json:"Err"`
				ContainerStatus *struct {
					ContainerID string `json:"ContainerID"`
					ExitCode    int64  `json:"ExitCode"`
				} `json:"ContainerStatus"`
			}{State: "running"}},
			{ID: "t2", ServiceID: "svc1", NodeID: "n1", DesiredState: "running", Status: struct {
				State           string `json:"State"`
				Message         string `json:"Message"`
				Err             string `json:"Err"`
				ContainerStatus *struct {
					ContainerID string `json:"ContainerID"`
					ExitCode    int64  `json:"ExitCode"`
				} `json:"ContainerStatus"`
			}{State: "failed"}},
		},
		"/networks": []network{{ID: "netA", Name: "frontend", Driver: "overlay", Scope: "swarm",
			IPAM: struct {
				Config []struct {
					Subnet  string `json:"Subnet"`
					Gateway string `json:"Gateway"`
				} `json:"Config"`
			}{Config: []struct {
				Subnet  string `json:"Subnet"`
				Gateway string `json:"Gateway"`
			}{{Subnet: "10.0.1.0/24"}}}}},
	})

	p := newTestProbe(t, handler)
	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if up, _ := value(t, points, "senhub.swarm.up", nil); up != 1 {
		t.Error("a manager must report up=1")
	}
	if v, _ := value(t, points, "swarm.cluster.quorum", nil); v != 1 {
		t.Error("a single reachable manager is quorum")
	}
	if v, _ := value(t, points, "swarm.service.replicas.desired", map[string]string{"swarm.service.name": "api"}); v != 3 {
		t.Errorf("desired replicas = %v, want 3", v)
	}
	if v, _ := value(t, points, "swarm.service.replicas.running", map[string]string{"swarm.service.name": "api"}); v != 1 {
		t.Errorf("running replicas = %v, want 1 (only one task is running)", v)
	}
	if v, _ := value(t, points, "swarm.service.converged", map[string]string{"swarm.service.name": "api"}); v != 0 {
		t.Error("1 of 3 replicas must not read as converged")
	}
	if v, _ := value(t, points, "swarm.service.tasks.failed", map[string]string{"swarm.service.name": "api"}); v != 1 {
		t.Errorf("failed tasks = %v, want 1", v)
	}
	// The overlay map: the service is on the segment, with the VIP its peers
	// resolve — the address a reachability problem is actually about.
	if v, ok := value(t, points, "swarm.service.network.attached", map[string]string{
		"swarm.service.name": "api", "swarm.network.name": "frontend", "swarm.service.vip": "10.0.1.5/24",
	}); !ok || v != 1 {
		t.Error("the service/overlay attachment with its VIP is missing")
	}
	if v, _ := value(t, points, "swarm.network.address.capacity", map[string]string{"swarm.network.name": "frontend"}); v != 254 {
		t.Errorf("overlay capacity = %v, want 254", v)
	}

	// The cluster entity and the overlay segment it declares.
	obs, ok := p.entitySrc.Observe()
	if !ok {
		t.Fatal("no observation after a successful manager cycle")
	}
	var cluster, segment *entity.Entity
	for i := range obs.Entities {
		switch obs.Entities[i].Type {
		case entity.TypeServiceInstance:
			cluster = &obs.Entities[i]
		case entity.TypeNetworkSegment:
			segment = &obs.Entities[i]
		}
	}
	if cluster == nil {
		t.Fatal("the cluster entity is missing after a successful manager cycle")
	}
	if id, _ := cluster.ID["service.instance.id"].(string); id != "swarm://cluster-xyz" {
		t.Errorf("cluster identity = %q, want swarm://cluster-xyz", id)
	}
	if segment == nil {
		t.Fatal("the overlay segment is missing")
	}
	// Subtype-prefixed: a bare network id says nothing about which authority
	// assigned it, and only swarm: is frozen (ADR 0034).
	if id, _ := segment.ID["network.segment.id"].(string); id != "swarm:netA" {
		t.Errorf("segment identity = %q, want swarm:netA", id)
	}

	// A segment must never travel without the edge that says whose it is.
	var declared bool
	for _, rel := range obs.Relations {
		if rel.Type == entity.RelHasSegment &&
			rel.FromID["service.instance.id"] == "swarm://cluster-xyz" &&
			rel.ToID["network.segment.id"] == "swarm:netA" {
			declared = true
		}
	}
	if !declared {
		t.Error("the segment carries no has_segment edge; nobody can say which cluster it belongs to")
	}
}

// A node that stops being a manager must withdraw the entity rather than keep
// republishing the last cluster it saw as though it were current.
func TestCollect_LosingManagerRoleWithdrawsTheEntity(t *testing.T) {
	manager := true
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/swarm") && !manager {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("This node is not a swarm manager."))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/swarm"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ID": "c1"})
		default:
			_ = json.NewEncoder(w).Encode([]any{})
		}
	})

	p := newTestProbe(t, handler)
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if _, ok := p.entitySrc.Observe(); !ok {
		t.Fatal("no entity after a manager cycle")
	}

	manager = false
	if _, err := p.Collect(); err != nil {
		t.Fatalf("Collect after demotion: %v", err)
	}
	if _, ok := p.entitySrc.Observe(); ok {
		t.Error("the cluster entity survived the loss of the manager role")
	}
}

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.SocketPath != dockerdial.DefaultAddress() {
		t.Errorf("socket = %q, want %q", cfg.SocketPath, dockerdial.DefaultAddress())
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("interval = %v, want %v", cfg.Interval, defaultInterval)
	}

	// A YAML loader hands back int or float64 for a bare number; both spellings
	// must produce the same duration.
	for _, v := range []any{30, float64(30), int64(30)} {
		cfg, _ := parseConfig(map[string]interface{}{"interval": v})
		if cfg.Interval != 30*time.Second {
			t.Errorf("interval %T(%v) parsed as %v, want 30s", v, v, cfg.Interval)
		}
	}
}

// A list endpoint that fails must not be reported as an empty cluster.
//
// Found in the field, not by reading the code: killing a manager to break
// quorum made the whole Engine API hang, which the probe reported correctly —
// and looking at the adjacent case showed that a PARTIAL failure (swarm info
// answers, /nodes does not) would emit swarm.cluster.nodes=0 and quorum=0.
// Zero machines is a confident, wrong statement; it is the same healthy-looking
// zero the worker path exists to avoid.
func TestCollect_AFailedListIsNotAnEmptyCluster(t *testing.T) {
	p := newTestProbe(t, engine(t, map[string]any{
		"/swarm": map[string]any{"ID": "c1"},
		"/nodes": errBody{http.StatusInternalServerError, "rpc error: the swarm does not have a leader"},
		// services/tasks/networks are absent from the routes on purpose: the
		// handler answers 404, which is also a failed read.
	}))

	points, err := p.Collect()
	if err == nil {
		t.Error("a failed list must surface as an error, not be swallowed")
	}
	if up, _ := value(t, points, "senhub.swarm.up", nil); up != 1 {
		t.Error("the swarm itself answered, so up must stay 1 — the failure is per-endpoint")
	}
	for _, name := range []string{
		"swarm.cluster.nodes", "swarm.cluster.managers", "swarm.cluster.quorum",
		"swarm.service.replicas.desired", "swarm.network.services",
	} {
		if v, ok := value(t, points, name, nil); ok {
			t.Errorf("%s was emitted as %v after a failed read; an unreadable cluster must emit nothing rather than zero", name, v)
		}
	}
}
