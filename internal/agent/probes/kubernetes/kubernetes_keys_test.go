package kubernetes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
)

func keysTagValue(ts []tags.Tag, key string) (string, bool) {
	for _, tag := range ts {
		if tag.Key == key {
			return tag.Value, true
		}
	}
	return "", false
}

func findPoint(t *testing.T, points []data_store.DataPoint, name string) data_store.DataPoint {
	t.Helper()
	for _, dp := range points {
		if dp.Name == name {
			return dp
		}
	}
	t.Fatalf("datapoint %q not found in %d points", name, len(points))
	return data_store.DataPoint{}
}

func newKubernetesProbeWithFakeClient(t *testing.T, objects ...runtime.Object) *KubernetesProbe {
	t.Helper()
	p := &KubernetesProbe{
		BaseProbe: &types.BaseProbe{},
		cfg: probeConfig{
			CollectNodes:      true,
			CollectPods:       true,
			CollectContainers: true,
			IncludeNamespaces: []string{"default"},
			ExcludeNamespaces: map[string]bool{},
			Interval:          time.Minute,
		},
		clientset: fake.NewSimpleClientset(objects...),
		entitySrc: newK8sEntitySource("test-cluster"),
	}
	p.SetProbeType(ProbeType)
	p.clusterEndpoint = "test-cluster"
	p.entitySrc.setClusterIdentity("test-cluster", "cluster-uid-1")
	return p
}

func TestKubernetesCollectCarriesEntityIdentityKeysAndCheckResult(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1", UID: "node-uid-1"},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				MachineID: "8b86170405bc4382b0577eac3df5e730",
			},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default", UID: "pod-uid-1"},
		Spec: corev1.PodSpec{
			NodeName:   "node-1",
			Containers: []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", Ready: true, RestartCount: 0, ContainerID: "containerd://abcdef"},
			},
		},
	}

	probe := newKubernetesProbeWithFakeClient(t, node, pod)
	points, err := probe.Collect()
	if err != nil {
		t.Fatal(err)
	}

	var sawNode, sawPod, sawContainer, sawUp, sawCheck bool
	for _, dp := range points {
		switch {
		case strings.HasPrefix(dp.Name, "k8s.node."):
			uid, ok := keysTagValue(dp.Tags, "k8s.node.uid")
			if !ok || uid != "node-uid-1" {
				t.Errorf("%s: k8s.node.uid = %q, want node-uid-1", dp.Name, uid)
			}
			sawNode = true
		case strings.HasPrefix(dp.Name, "k8s.pod."):
			uid, ok := keysTagValue(dp.Tags, "k8s.pod.uid")
			if !ok || uid != "pod-uid-1" {
				t.Errorf("%s: k8s.pod.uid = %q, want pod-uid-1", dp.Name, uid)
			}
			sawPod = true
		case strings.HasPrefix(dp.Name, "k8s.container."):
			containerID, ok := keysTagValue(dp.Tags, "container.id")
			if !ok || containerID != "abcdef" {
				t.Errorf("%s: container.id = %q, want abcdef", dp.Name, containerID)
			}
			sawContainer = true
		case dp.Name == metricUp:
			clusterID, ok := keysTagValue(dp.Tags, "service.instance.id")
			if !ok || clusterID != "cluster-uid-1" {
				t.Errorf("%s: service.instance.id = %q, want cluster-uid-1", dp.Name, clusterID)
			}
			sawUp = true
		case dp.Name == dbcommon.CheckResultMetric:
			if dp.Value != 0 {
				t.Errorf("check result = %v, want 0", dp.Value)
			}
			metricType, _ := keysTagValue(dp.Tags, "metric_type")
			if metricType != "status" {
				t.Errorf("check result metric_type = %q, want status", metricType)
			}
			clusterID, ok := keysTagValue(dp.Tags, "service.instance.id")
			if !ok || clusterID != "cluster-uid-1" {
				t.Errorf("check result service.instance.id = %q, want cluster-uid-1", clusterID)
			}
			sawCheck = true
		}
	}

	if !sawNode || !sawPod || !sawContainer || !sawUp || !sawCheck {
		t.Fatalf("missing datapoints: node=%v pod=%v container=%v up=%v check=%v",
			sawNode, sawPod, sawContainer, sawUp, sawCheck)
	}
}

func TestKubernetesNodeListErrorClassifiesTargetDown(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.Fake.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("dial tcp: connection refused")
	})

	probe := &KubernetesProbe{
		clientset: cs,
		cfg:       probeConfig{CollectNodes: true},
	}

	_, err := probe.collectNodes(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected a node collection error")
	}
	if got := dbcommon.ClassifyError(err); got != dbcommon.CheckTargetDown {
		t.Fatalf("ClassifyError = %d, want CheckTargetDown (%d)", got, dbcommon.CheckTargetDown)
	}
}
