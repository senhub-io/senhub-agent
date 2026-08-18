package kubernetes

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"senhub-agent.go/internal/agent/services/entity"
)

// A node keyed on its MachineID is byte-identical to the entity an agent
// running inside it emits, so the two converge on one graph node instead of
// duplicating. That convergence is the point of modelling the node at all.
func TestNodeEntity_KeyedOnTheMachineID(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{
			MachineID:       "8b86170405bc4382b0577eac3df5e730",
			SystemUUID:      "4C4C4544-0043",
			OperatingSystem: "linux",
			KubeletVersion:  "v1.31.2",
		}},
	}

	ent, ok := nodeEntity(n, "cluster-uid")
	if !ok {
		t.Fatal("node with a MachineID must produce an entity")
	}
	if ent.Type != entity.TypeHost {
		t.Errorf("type = %q, want host — a Kubernetes node is a machine with its own OS", ent.Type)
	}
	// The dashed form, not the raw file contents: gopsutil formats the same
	// bytes as a UUID for the agent's own host.id, and two spellings of one
	// machine are two entities.
	if got := ent.ID["host.id"]; got != "8b861704-05bc-4382-b057-7eac3df5e730" {
		t.Errorf("host.id = %v, want the dashed UUID form the agent emits", got)
	}
	// SystemUUID is descriptive, never the identity: it is absent or forged on
	// several virtualisation platforms.
	if got := ent.Attributes["host.uuid"]; got != "4C4C4544-0043" {
		t.Errorf("host.uuid attribute = %v, want the SystemUUID", got)
	}
	if _, isID := ent.ID["host.uuid"]; isID {
		t.Error("SystemUUID must not be part of the identity")
	}
}

// The consumer's instruction, and the right one: no /etc/machine-id, no host
// entity from the cluster view. A missing node is a visible hole an in-guest
// agent fills; a node keyed on a fallback is a silent duplicate nobody sees.
func TestNodeEntity_NoMachineIDMeansNoEntity(t *testing.T) {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-2"},
		Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{
			MachineID:  "",
			SystemUUID: "4C4C4544-0043",
		}},
	}
	if _, ok := nodeEntity(n, "cluster-uid"); ok {
		t.Fatal("a node without a MachineID must produce NO entity — falling back " +
			"on SystemUUID would mint a second identity for a machine the " +
			"in-guest agent already reports correctly")
	}
}

// The runtime scheme names the path by which the container was observed, not
// the container. Keeping it would double every container that a docker probe
// on the node also reports.
func TestNormalizeContainerID_StripsTheRuntimeScheme(t *testing.T) {
	const sha = "9f2a1c4e7b3d5a6f8e0c2b4d6a8f1e3c5b7d9f0a2c4e6b8d0f2a4c6e8b0d2f4a"

	for _, raw := range []string{
		"containerd://" + sha,
		"docker://" + sha,
		"cri-o://" + sha,
		sha,
		"  containerd://" + sha + "  ",
	} {
		if got := normalizeContainerID(raw); got != sha {
			t.Errorf("normalizeContainerID(%q) = %q, want the bare sha", raw, got)
		}
	}

	// Uppercase is normalised: the canonical form in the consumer's graph is
	// lowercase, and two spellings of one id are two entities.
	if got := normalizeContainerID("containerd://ABCDEF"); got != "abcdef" {
		t.Errorf("uppercase not normalised, got %q", got)
	}

	for _, empty := range []string{"", "   ", "containerd://"} {
		if got := normalizeContainerID(empty); got != "" {
			t.Errorf("normalizeContainerID(%q) = %q, want empty — an entity with a "+
				"fabricated identity is worse than none", empty, got)
		}
	}
}

// A container that has never started has no runtime id, so it is not a
// container yet.
func TestContainerEntity_NoRuntimeIDMeansNoEntity(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}
	cs := &corev1.ContainerStatus{Name: "app", ContainerID: ""}

	if _, ok := containerEntity(cs, pod, "node-machine-id"); ok {
		t.Fatal("a container with no runtime id must produce no entity")
	}
}

func TestContainerEntity_CarriesTheCanonicalIdentity(t *testing.T) {
	const sha = "9f2a1c4e7b3d5a6f8e0c2b4d6a8f1e3c5b7d9f0a2c4e6b8d0f2a4c6e8b0d2f4a"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-abc", Namespace: "prod"},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
	}
	cs := &corev1.ContainerStatus{Name: "app", Image: "nginx:1.27", ContainerID: "containerd://" + sha}

	ent, ok := containerEntity(cs, pod, "machine-1")
	if !ok {
		t.Fatal("expected an entity")
	}
	if ent.Type != entity.TypeContainer {
		t.Errorf("type = %q, want container", ent.Type)
	}
	if got := ent.ID["container.id"]; got != sha {
		t.Errorf("container.id = %v, want the bare sha", got)
	}
	// The runtime is kept — it is a fact worth having, it simply is not part
	// of the identity.
	if got := ent.Attributes["container.runtime"]; got != "containerd" {
		t.Errorf("container.runtime = %v, want containerd", got)
	}
}

// The identity is the UID, never namespace/name: the pair is editable and
// reused, so a pod recreated under the same name would inherit the history of
// a different one.
func TestPodEntity_KeyedOnTheUID(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api-abc", Namespace: "prod", UID: "9c1f-uid"},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
	}
	ent, ok := podEntity(pod)
	if !ok {
		t.Fatal("expected a pod entity")
	}
	if ent.Type != entity.TypePod {
		t.Errorf("type = %q, want pod", ent.Type)
	}
	if got := ent.ID["k8s.pod.uid"]; got != "9c1f-uid" {
		t.Errorf("identity = %v, want the UID", got)
	}
	if _, isID := ent.ID["k8s.pod.name"]; isID {
		t.Error("the name must be descriptive, never part of the identity")
	}

	if _, ok := podEntity(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x"}}); ok {
		t.Error("a pod with no UID must produce no entity")
	}
}

// The chain is container -> pod -> host. runs_on propagates failure from
// target to source, so a node going down takes its pods, which take their
// containers — an impact query answers transitively with no extra code.
func TestRunsOn_BuildsTheChainAndSkipsEmptyTargets(t *testing.T) {
	podID := map[string]any{"k8s.pod.uid": "uid-1"}
	ctrID := map[string]any{"container.id": "sha"}
	hostID := map[string]any{"host.id": "machine-1"}

	rel, ok := runsOn(entity.TypeContainer, ctrID, entity.TypePod, podID)
	if !ok {
		t.Fatal("expected container -> pod")
	}
	if rel.FromType != entity.TypeContainer || rel.ToType != entity.TypePod {
		t.Errorf("edge is %s -> %s, want container -> pod", rel.FromType, rel.ToType)
	}
	if rel.Type != entity.RelRunsOn {
		t.Errorf("relation type = %q, want runs_on", rel.Type)
	}

	if _, ok := runsOn(entity.TypePod, podID, entity.TypeHost, hostID); !ok {
		t.Error("expected pod -> host")
	}

	// An edge whose target never materialises is buffered then dropped by the
	// consumer: emitting it costs a warning and buys nothing.
	for _, bad := range []map[string]any{nil, {}, {"host.id": ""}, {"host.id": "  "}} {
		if _, ok := runsOn(entity.TypePod, podID, entity.TypeHost, bad); ok {
			t.Errorf("edge to an empty identity %v must be skipped", bad)
		}
	}
}

// The duplicate that nearly shipped.
//
// Kubernetes returns /etc/machine-id verbatim; gopsutil formats the same bytes
// as a dashed UUID for the agent's own host.id. Measured on a real machine:
// 8b86170405bc4382b0577eac3df5e730 against
// 8b861704-05bc-4382-b057-7eac3df5e730. Same file, two spellings, and
// therefore a silent duplicate for every node in every cluster.
//
// Checking that both sides read the same FILE was not enough. Only comparing
// the emitted STRINGS caught it.
func TestCanonicalMachineID_MatchesTheAgentSpelling(t *testing.T) {
	const raw = "8b86170405bc4382b0577eac3df5e730"
	const want = "8b861704-05bc-4382-b057-7eac3df5e730"

	if got := canonicalMachineID(raw); got != want {
		t.Errorf("canonicalMachineID(%q) = %q, want %q — the agent emits the "+
			"dashed form, and a different spelling is a different entity", raw, got, want)
	}
	// Already dashed: left alone rather than mangled.
	if got := canonicalMachineID(want); got != want {
		t.Errorf("an already-canonical id must pass through unchanged, got %q", got)
	}
	// Uppercase hex normalises, since case is another way to spell one id twice.
	if got := canonicalMachineID("8B86170405BC4382B0577EAC3DF5E730"); got != want {
		t.Errorf("uppercase not normalised, got %q", got)
	}
	// An unexpected shape is NOT reformatted into a plausible-looking identity
	// that would be wrong; it is returned as-is for the caller to judge.
	for _, odd := range []string{"", "short", "not-hex-but-exactly-32-chars-xx!"} {
		if got := canonicalMachineID(odd); got != strings.TrimSpace(odd) {
			t.Errorf("canonicalMachineID(%q) = %q, want it returned unchanged", odd, got)
		}
	}
}

// A pod's owning workload is the first thing anyone wants to know about it, and
// it is read from the owner reference rather than parsed out of the pod name —
// the name's shape is a convention, not a contract.
func TestPodOwner_ReportsTheDeploymentNotTheReplicaSet(t *testing.T) {
	ctrl := true
	pod := &corev1.Pod{}
	pod.OwnerReferences = []metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "api-7d75d55c8f", Controller: &ctrl},
	}
	// Nobody thinks in ReplicaSets: Kubernetes inserts one between a Deployment
	// and its pods, and an operator asks about the Deployment.
	if kind, name := podOwner(pod); kind != "Deployment" || name != "api" {
		t.Errorf("owner = %s/%s, want Deployment/api", kind, name)
	}
}

// A workload that owns its pods directly is reported as itself.
func TestPodOwner_KeepsDirectOwners(t *testing.T) {
	ctrl := true
	for _, c := range []struct{ kind, name string }{
		{"StatefulSet", "db"},
		{"DaemonSet", "node-exporter"},
		{"Job", "migrate"},
	} {
		pod := &corev1.Pod{}
		pod.OwnerReferences = []metav1.OwnerReference{{Kind: c.kind, Name: c.name, Controller: &ctrl}}
		if kind, name := podOwner(pod); kind != c.kind || name != c.name {
			t.Errorf("owner = %s/%s, want %s/%s", kind, name, c.kind, c.name)
		}
	}
}

// A hand-made ReplicaSet must keep its own name rather than being renamed into
// a Deployment that does not exist.
func TestPodOwner_DoesNotInventADeployment(t *testing.T) {
	ctrl := true
	pod := &corev1.Pod{}
	pod.OwnerReferences = []metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "standalone", Controller: &ctrl},
	}
	if kind, name := podOwner(pod); kind != "ReplicaSet" || name != "standalone" {
		t.Errorf("owner = %s/%s, want ReplicaSet/standalone", kind, name)
	}
}

// A pod created directly has no owner, and inventing one would be worse than
// saying nothing.
func TestPodOwner_EmptyWhenThereIsNone(t *testing.T) {
	if kind, name := podOwner(&corev1.Pod{}); kind != "" || name != "" {
		t.Errorf("owner = %s/%s, want empty", kind, name)
	}
}
