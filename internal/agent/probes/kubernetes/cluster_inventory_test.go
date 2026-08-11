package kubernetes

import (
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
			MachineID:       "5f2c1b8e4a7d",
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
	if got := ent.ID["host.id"]; got != "5f2c1b8e4a7d" {
		t.Errorf("host.id = %v, want the MachineID", got)
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

// The placement edge is skipped when the node contributed no host entity: a
// relation whose target never materialises is buffered then dropped by the
// consumer, costing a warning and buying nothing.
func TestContainerRunsOnNode_SkippedWithoutATarget(t *testing.T) {
	if _, ok := containerRunsOnNode("sha", ""); ok {
		t.Error("no node identity must mean no edge")
	}
	if _, ok := containerRunsOnNode("", "machine-1"); ok {
		t.Error("no container identity must mean no edge")
	}

	rel, ok := containerRunsOnNode("sha", "machine-1")
	if !ok {
		t.Fatal("expected an edge")
	}
	if rel.Type != entity.RelRunsOn {
		t.Errorf("relation type = %q, want runs_on", rel.Type)
	}
	if rel.ToType != entity.TypeHost || rel.FromType != entity.TypeContainer {
		t.Errorf("edge is %s -> %s, want container -> host", rel.FromType, rel.ToType)
	}
}
