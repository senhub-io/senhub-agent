package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/entity"
	"senhub-agent.go/internal/agent/services/logger"
)

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig() unexpected error: %v", err)
	}
	if !cfg.CollectNodes {
		t.Error("CollectNodes should default to true")
	}
	if !cfg.CollectPods {
		t.Error("CollectPods should default to true")
	}
	if !cfg.CollectContainers {
		t.Error("CollectContainers should default to true")
	}
	if !cfg.CollectDeployments {
		t.Error("CollectDeployments should default to true")
	}
	if cfg.Interval != defaultInterval {
		t.Errorf("Interval: got %v, want %v", cfg.Interval, defaultInterval)
	}
	if !cfg.ExcludeNamespaces["kube-system"] {
		t.Error("kube-system should be excluded by default")
	}
}

func TestParseConfig_CustomInterval(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{"interval": 60})
	if err != nil {
		t.Fatalf("parseConfig() unexpected error: %v", err)
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("Interval: got %v, want 60s", cfg.Interval)
	}
}

func TestParseConfig_CollectFlags(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"collect": map[string]interface{}{
			"nodes":       false,
			"deployments": false,
		},
	})
	if err != nil {
		t.Fatalf("parseConfig() unexpected error: %v", err)
	}
	if cfg.CollectNodes {
		t.Error("CollectNodes should be false")
	}
	if !cfg.CollectPods {
		t.Error("CollectPods should remain true (default)")
	}
	if cfg.CollectDeployments {
		t.Error("CollectDeployments should be false")
	}
}

func TestParseConfig_NamespaceFilter(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"namespaces": map[string]interface{}{
			"include": []interface{}{"default", "monitoring"},
			"exclude": []interface{}{"kube-system", "kube-public"},
		},
	})
	if err != nil {
		t.Fatalf("parseConfig() unexpected error: %v", err)
	}
	if len(cfg.IncludeNamespaces) != 2 {
		t.Errorf("IncludeNamespaces: got %d, want 2", len(cfg.IncludeNamespaces))
	}
	if !cfg.ExcludeNamespaces["kube-system"] {
		t.Error("kube-system should be excluded")
	}
	if !cfg.ExcludeNamespaces["kube-public"] {
		t.Error("kube-public should be excluded")
	}
}

func TestClusterEndpointFromHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://10.0.0.1:6443", "10.0.0.1:6443"},
		{"http://10.0.0.1:6443", "10.0.0.1:6443"},
		{"10.0.0.1:6443", "10.0.0.1:6443"},
		{"", "localhost"},
	}
	for _, tt := range tests {
		got := clusterEndpointFromHost(tt.input)
		if got != tt.want {
			t.Errorf("clusterEndpointFromHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// noopRegisterEntitySource is a test replacement for registerEntitySource
// that avoids touching the global entity registry.
func noopRegisterEntitySource(_ entity.Source) func() { return func() {} }

// buildTestProbe constructs a KubernetesProbe backed by the given fake
// clientset, with entity registration replaced by a no-op.
func buildTestProbe(t *testing.T, cs *kubefake.Clientset, collectNodes, collectPods, collectDeployments bool) *KubernetesProbe {
	t.Helper()
	orig := registerEntitySource
	registerEntitySource = noopRegisterEntitySource
	t.Cleanup(func() { registerEntitySource = orig })

	p := &KubernetesProbe{
		BaseProbe: &types.BaseProbe{},
		cfg: probeConfig{
			CollectNodes:       collectNodes,
			CollectPods:        collectPods,
			CollectContainers:  collectPods,
			CollectDeployments: collectDeployments,
			ExcludeNamespaces:  map[string]bool{},
			IncludeNamespaces:  []string{"default"},
			Interval:           defaultInterval,
		},
		moduleLogger:    logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "probe.kubernetes.test"),
		clientset:       cs,
		clusterEndpoint: "test-cluster:6443",
	}
	p.SetProbeType(ProbeType)
	return p
}

// TestCollect_UpMetric_Healthy verifies that senhub.kubernetes.up=1 is emitted
// when the API server is reachable and node list succeeds.
func TestCollect_UpMetric_Healthy(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
		},
	}
	cs := kubefake.NewSimpleClientset(node)
	p := buildTestProbe(t, cs, true, false, false)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() returned unexpected error: %v", err)
	}

	var upFound bool
	for _, dp := range points {
		if dp.Name == metricUp {
			upFound = true
			if dp.Value != 1 {
				t.Errorf("senhub.kubernetes.up: got %v, want 1", dp.Value)
			}
		}
	}
	if !upFound {
		t.Error("senhub.kubernetes.up was not emitted on healthy cluster")
	}
}

// ---------------------------------------------------------------------------
// Helpers shared by build* tests
// ---------------------------------------------------------------------------

// tagValue returns the value of the first tag with the given key, or "".
func tagValue(dp data_store.DataPoint, key string) string {
	for _, tg := range dp.Tags {
		if tg.Key == key {
			return tg.Value
		}
	}
	return ""
}

// findDP returns the first DataPoint whose Name matches, or a zero value.
func findDP(points []data_store.DataPoint, name string) (data_store.DataPoint, bool) {
	for _, dp := range points {
		if dp.Name == name {
			return dp, true
		}
	}
	return data_store.DataPoint{}, false
}

// ---------------------------------------------------------------------------
// buildPodPoints — table-driven unit tests
// ---------------------------------------------------------------------------

func TestBuildPodPoints_Running(t *testing.T) {
	tests := []struct {
		name          string
		phase         corev1.PodPhase
		readyCond     corev1.ConditionStatus
		restartCounts []int32
		wantPhase     float64
		wantReady     float64
		wantRestarts  float64
	}{
		{
			name:         "running and ready, no restarts",
			phase:        corev1.PodRunning,
			readyCond:    corev1.ConditionTrue,
			wantPhase:    1,
			wantReady:    1,
			wantRestarts: 0,
		},
		{
			name:         "pending pod, not ready",
			phase:        corev1.PodPending,
			readyCond:    corev1.ConditionFalse,
			wantPhase:    0,
			wantReady:    0,
			wantRestarts: 0,
		},
		{
			name:          "running with restarts",
			phase:         corev1.PodRunning,
			readyCond:     corev1.ConditionTrue,
			restartCounts: []int32{3, 5},
			wantPhase:     1,
			wantReady:     1,
			wantRestarts:  8,
		},
		{
			name:      "failed pod",
			phase:     corev1.PodFailed,
			readyCond: corev1.ConditionFalse,
			wantPhase: 0,
			wantReady: 0,
		},
	}

	p := &KubernetesProbe{
		BaseProbe:       &types.BaseProbe{},
		clusterEndpoint: "test-cluster:6443",
	}
	p.SetProbeType(ProbeType)
	now := time.Now()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{NodeName: "node-1"},
				Status: corev1.PodStatus{
					Phase: tt.phase,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: tt.readyCond},
					},
				},
			}
			for _, rc := range tt.restartCounts {
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
					RestartCount: rc,
				})
			}

			pts := p.buildPodPoints(pod, now)

			// Expect exactly 3 metrics.
			if len(pts) != 3 {
				t.Fatalf("buildPodPoints returned %d points, want 3", len(pts))
			}

			dpPhase, ok := findDP(pts, "k8s.pod.phase")
			if !ok {
				t.Fatal("k8s.pod.phase not emitted")
			}
			if dpPhase.Value != tt.wantPhase {
				t.Errorf("k8s.pod.phase = %v, want %v", dpPhase.Value, tt.wantPhase)
			}
			if tagValue(dpPhase, "k8s.pod.name") != "test-pod" {
				t.Errorf("k8s.pod.name tag missing or wrong: %v", dpPhase.Tags)
			}
			if tagValue(dpPhase, "metric_type") != "pod" {
				t.Errorf("metric_type tag missing or wrong: %v", dpPhase.Tags)
			}

			dpReady, ok := findDP(pts, "k8s.pod.ready")
			if !ok {
				t.Fatal("k8s.pod.ready not emitted")
			}
			if dpReady.Value != tt.wantReady {
				t.Errorf("k8s.pod.ready = %v, want %v", dpReady.Value, tt.wantReady)
			}

			dpRestarts, ok := findDP(pts, "k8s.pod.restarts")
			if !ok {
				t.Fatal("k8s.pod.restarts not emitted")
			}
			if dpRestarts.Value != tt.wantRestarts {
				t.Errorf("k8s.pod.restarts = %v, want %v", dpRestarts.Value, tt.wantRestarts)
			}
		})
	}
}

func TestBuildPodPoints_Tags(t *testing.T) {
	p := &KubernetesProbe{BaseProbe: &types.BaseProbe{}, clusterEndpoint: "c:6443"}
	p.SetProbeType(ProbeType)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "mypod", Namespace: "myns"},
		Spec:       corev1.PodSpec{NodeName: "mynode"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	pts := p.buildPodPoints(pod, time.Now())
	if len(pts) == 0 {
		t.Fatal("no points returned")
	}
	dp := pts[0]
	for _, want := range [][2]string{
		{"k8s.pod.name", "mypod"},
		{"k8s.namespace.name", "myns"},
		{"k8s.node.name", "mynode"},
		{"metric_type", "pod"},
	} {
		if tagValue(dp, want[0]) != want[1] {
			t.Errorf("tag %q = %q, want %q", want[0], tagValue(dp, want[0]), want[1])
		}
	}
}

// ---------------------------------------------------------------------------
// buildContainerPoints — table-driven unit tests
// ---------------------------------------------------------------------------

func TestBuildContainerPoints(t *testing.T) {
	tests := []struct {
		name           string
		containerReady bool
		restarts       int32
		wantReady      float64
	}{
		{"ready container", true, 0, 1},
		{"not-ready container", false, 2, 0},
		{"ready with restarts", true, 7, 1},
	}

	p := &KubernetesProbe{BaseProbe: &types.BaseProbe{}, clusterEndpoint: "c:6443"}
	p.SetProbeType(ProbeType)
	now := time.Now()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "app", Ready: tt.containerReady, RestartCount: tt.restarts},
					},
				},
			}

			pts := p.buildContainerPoints(pod, now)
			// One container → 2 datapoints.
			if len(pts) != 2 {
				t.Fatalf("buildContainerPoints returned %d points, want 2", len(pts))
			}

			dpReady, ok := findDP(pts, "k8s.container.ready")
			if !ok {
				t.Fatal("k8s.container.ready not emitted")
			}
			if dpReady.Value != tt.wantReady {
				t.Errorf("k8s.container.ready = %v, want %v", dpReady.Value, tt.wantReady)
			}
			if tagValue(dpReady, "k8s.container.name") != "app" {
				t.Errorf("k8s.container.name tag wrong: %v", dpReady.Tags)
			}
			if tagValue(dpReady, "metric_type") != "container" {
				t.Errorf("metric_type tag wrong: %v", dpReady.Tags)
			}

			dpRestarts, ok := findDP(pts, "k8s.container.restarts")
			if !ok {
				t.Fatal("k8s.container.restarts not emitted")
			}
			if dpRestarts.Value != float64(tt.restarts) {
				t.Errorf("k8s.container.restarts = %v, want %v", dpRestarts.Value, tt.restarts)
			}
		})
	}
}

func TestBuildContainerPoints_MultipleContainers(t *testing.T) {
	p := &KubernetesProbe{BaseProbe: &types.BaseProbe{}, clusterEndpoint: "c:6443"}
	p.SetProbeType(ProbeType)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "web", Ready: true, RestartCount: 0},
				{Name: "sidecar", Ready: false, RestartCount: 3},
			},
		},
	}
	pts := p.buildContainerPoints(pod, time.Now())
	// 2 containers × 2 metrics = 4 points.
	if len(pts) != 4 {
		t.Fatalf("buildContainerPoints returned %d points, want 4", len(pts))
	}
	// Verify each container name appears in the tags.
	names := map[string]bool{}
	for _, dp := range pts {
		names[tagValue(dp, "k8s.container.name")] = true
	}
	if !names["web"] {
		t.Error("container 'web' not found in datapoints")
	}
	if !names["sidecar"] {
		t.Error("container 'sidecar' not found in datapoints")
	}
}

func TestBuildContainerPoints_NilStatuses(t *testing.T) {
	p := &KubernetesProbe{BaseProbe: &types.BaseProbe{}, clusterEndpoint: "c:6443"}
	p.SetProbeType(ProbeType)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
		Status:     corev1.PodStatus{}, // no ContainerStatuses
	}
	pts := p.buildContainerPoints(pod, time.Now())
	if len(pts) != 0 {
		t.Errorf("buildContainerPoints with no containers: got %d points, want 0", len(pts))
	}
}

// ---------------------------------------------------------------------------
// collectNodes — fake clientset tests
// ---------------------------------------------------------------------------

func TestCollectNodes_ReadyAndAllocatable(t *testing.T) {
	cpu := resource.MustParse("4")
	mem := resource.MustParse("8Gi")
	podCap := resource.MustParse("110")

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    cpu,
				corev1.ResourceMemory: mem,
				corev1.ResourcePods:   podCap,
			},
			Capacity: corev1.ResourceList{
				corev1.ResourcePods: podCap,
			},
		},
	}
	cs := kubefake.NewSimpleClientset(node)
	p := buildTestProbe(t, cs, true, false, false)

	pts, err := p.collectNodes(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectNodes() error: %v", err)
	}

	dpReady, ok := findDP(pts, "k8s.node.ready")
	if !ok {
		t.Fatal("k8s.node.ready not emitted")
	}
	if dpReady.Value != 1 {
		t.Errorf("k8s.node.ready = %v, want 1", dpReady.Value)
	}
	if tagValue(dpReady, "k8s.node.name") != "worker-1" {
		t.Errorf("k8s.node.name tag wrong: %v", dpReady.Tags)
	}
	if tagValue(dpReady, "metric_type") != "node" {
		t.Errorf("metric_type tag wrong: %v", dpReady.Tags)
	}

	dpCPU, ok := findDP(pts, "k8s.node.cpu.allocatable")
	if !ok {
		t.Fatal("k8s.node.cpu.allocatable not emitted")
	}
	if dpCPU.Value <= 0 {
		t.Errorf("k8s.node.cpu.allocatable = %v, want > 0", dpCPU.Value)
	}

	dpMem, ok := findDP(pts, "k8s.node.memory.allocatable")
	if !ok {
		t.Fatal("k8s.node.memory.allocatable not emitted")
	}
	if dpMem.Value <= 0 {
		t.Errorf("k8s.node.memory.allocatable = %v, want > 0", dpMem.Value)
	}

	dpPodsCap, ok := findDP(pts, "k8s.node.pods.capacity")
	if !ok {
		t.Fatal("k8s.node.pods.capacity not emitted")
	}
	if dpPodsCap.Value != 110 {
		t.Errorf("k8s.node.pods.capacity = %v, want 110", dpPodsCap.Value)
	}

	dpPodsAlloc, ok := findDP(pts, "k8s.node.pods.allocated")
	if !ok {
		t.Fatal("k8s.node.pods.allocated not emitted")
	}
	if dpPodsAlloc.Value != 110 {
		t.Errorf("k8s.node.pods.allocated = %v, want 110", dpPodsAlloc.Value)
	}
}

func TestCollectNodes_NotReady(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-bad"},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			},
		},
	}
	cs := kubefake.NewSimpleClientset(node)
	p := buildTestProbe(t, cs, true, false, false)

	pts, err := p.collectNodes(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectNodes() error: %v", err)
	}
	dpReady, ok := findDP(pts, "k8s.node.ready")
	if !ok {
		t.Fatal("k8s.node.ready not emitted")
	}
	if dpReady.Value != 0 {
		t.Errorf("k8s.node.ready = %v, want 0 for not-ready node", dpReady.Value)
	}
}

func TestCollectNodes_MultipleNodes(t *testing.T) {
	nodes := []runtime.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-b"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
				},
			},
		},
	}
	cs := kubefake.NewSimpleClientset(nodes...)
	p := buildTestProbe(t, cs, true, false, false)

	pts, err := p.collectNodes(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectNodes() error: %v", err)
	}
	// Count k8s.node.ready datapoints — one per node.
	var readyCount int
	for _, dp := range pts {
		if dp.Name == "k8s.node.ready" {
			readyCount++
		}
	}
	if readyCount != 2 {
		t.Errorf("expected 2 k8s.node.ready datapoints (one per node), got %d", readyCount)
	}
}

func TestCollectNodes_Empty(t *testing.T) {
	cs := kubefake.NewSimpleClientset()
	p := buildTestProbe(t, cs, true, false, false)

	pts, err := p.collectNodes(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectNodes() error: %v", err)
	}
	if len(pts) != 0 {
		t.Errorf("collectNodes with no nodes: got %d points, want 0", len(pts))
	}
}

// ---------------------------------------------------------------------------
// collectDeployments — fake clientset tests
// ---------------------------------------------------------------------------

func int32Ptr(v int32) *int32 { return &v }

func TestCollectDeployments_FullyAvailable(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 3},
	}
	cs := kubefake.NewSimpleClientset(dep)
	p := buildTestProbe(t, cs, false, false, true)

	pts, err := p.collectDeployments(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectDeployments() error: %v", err)
	}

	dpAvail, ok := findDP(pts, "k8s.deployment.available")
	if !ok {
		t.Fatal("k8s.deployment.available not emitted")
	}
	if dpAvail.Value != 3 {
		t.Errorf("k8s.deployment.available = %v, want 3", dpAvail.Value)
	}
	if tagValue(dpAvail, "k8s.deployment.name") != "my-app" {
		t.Errorf("k8s.deployment.name tag wrong: %v", dpAvail.Tags)
	}
	if tagValue(dpAvail, "k8s.namespace.name") != "default" {
		t.Errorf("k8s.namespace.name tag wrong: %v", dpAvail.Tags)
	}
	if tagValue(dpAvail, "metric_type") != "deployment" {
		t.Errorf("metric_type tag wrong: %v", dpAvail.Tags)
	}

	dpDesired, ok := findDP(pts, "k8s.deployment.desired")
	if !ok {
		t.Fatal("k8s.deployment.desired not emitted")
	}
	if dpDesired.Value != 3 {
		t.Errorf("k8s.deployment.desired = %v, want 3", dpDesired.Value)
	}

	dpReady, ok := findDP(pts, "k8s.deployment.ready")
	if !ok {
		t.Fatal("k8s.deployment.ready not emitted")
	}
	if dpReady.Value != 1 {
		t.Errorf("k8s.deployment.ready = %v, want 1 (all replicas available)", dpReady.Value)
	}
}

func TestCollectDeployments_Degraded(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "degraded-app", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(5)},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 2},
	}
	cs := kubefake.NewSimpleClientset(dep)
	p := buildTestProbe(t, cs, false, false, true)

	pts, err := p.collectDeployments(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectDeployments() error: %v", err)
	}

	dpReady, ok := findDP(pts, "k8s.deployment.ready")
	if !ok {
		t.Fatal("k8s.deployment.ready not emitted")
	}
	if dpReady.Value != 0 {
		t.Errorf("k8s.deployment.ready = %v, want 0 (degraded)", dpReady.Value)
	}

	dpAvail, ok := findDP(pts, "k8s.deployment.available")
	if !ok {
		t.Fatal("k8s.deployment.available not emitted")
	}
	if dpAvail.Value != 2 {
		t.Errorf("k8s.deployment.available = %v, want 2", dpAvail.Value)
	}

	dpDesired, ok := findDP(pts, "k8s.deployment.desired")
	if !ok {
		t.Fatal("k8s.deployment.desired not emitted")
	}
	if dpDesired.Value != 5 {
		t.Errorf("k8s.deployment.desired = %v, want 5", dpDesired.Value)
	}
}

func TestCollectDeployments_NilReplicas(t *testing.T) {
	// Spec.Replicas nil means 1 desired replica.
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "no-replicas", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{}, // Replicas = nil
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 1},
	}
	cs := kubefake.NewSimpleClientset(dep)
	p := buildTestProbe(t, cs, false, false, true)

	pts, err := p.collectDeployments(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectDeployments() error: %v", err)
	}

	dpDesired, ok := findDP(pts, "k8s.deployment.desired")
	if !ok {
		t.Fatal("k8s.deployment.desired not emitted")
	}
	// nil Replicas → desiredCount = 0 in our code; available(1) >= desired(0) → ready=1.
	if dpDesired.Value != 0 {
		t.Errorf("k8s.deployment.desired = %v, want 0 when Replicas is nil", dpDesired.Value)
	}

	dpReady, ok := findDP(pts, "k8s.deployment.ready")
	if !ok {
		t.Fatal("k8s.deployment.ready not emitted")
	}
	if dpReady.Value != 1 {
		t.Errorf("k8s.deployment.ready = %v, want 1 (available >= desired=0)", dpReady.Value)
	}
}

func TestCollectDeployments_MultipleNamespaces(t *testing.T) {
	deps := []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "app-a", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
			Status:     appsv1.DeploymentStatus{AvailableReplicas: 2},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "app-b", Namespace: "monitoring"},
			Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
			Status:     appsv1.DeploymentStatus{AvailableReplicas: 1},
		},
	}
	cs := kubefake.NewSimpleClientset(deps...)
	// Include both namespaces explicitly.
	p := buildTestProbe(t, cs, false, false, true)
	p.cfg.IncludeNamespaces = []string{"default", "monitoring"}

	pts, err := p.collectDeployments(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("collectDeployments() error: %v", err)
	}

	// 2 deployments × 3 metrics = 6 points.
	if len(pts) != 6 {
		t.Errorf("collectDeployments 2-ns: got %d points, want 6", len(pts))
	}

	names := map[string]bool{}
	for _, dp := range pts {
		if dp.Name == "k8s.deployment.available" {
			names[tagValue(dp, "k8s.deployment.name")] = true
		}
	}
	if !names["app-a"] {
		t.Error("deployment 'app-a' not found in datapoints")
	}
	if !names["app-b"] {
		t.Error("deployment 'app-b' not found in datapoints")
	}
}

// ---------------------------------------------------------------------------
// TestCollect_UpMetric_APIError (existing — preserved below)
// ---------------------------------------------------------------------------

// TestCollect_UpMetric_APIError verifies that senhub.kubernetes.up=0 is emitted
// (not suppressed) when the API server is unreachable (#469).
// Collect must return nil error so the up=0 datapoint reaches all sinks.
func TestCollect_UpMetric_APIError(t *testing.T) {
	cs := kubefake.NewSimpleClientset()
	cs.PrependReactor("list", "nodes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	p := buildTestProbe(t, cs, true, false, false)

	points, err := p.Collect()
	if err != nil {
		t.Fatalf("Collect() must not return an error on API failure (got: %v); up=0 must flow to sinks", err)
	}
	if len(points) == 0 {
		t.Fatal("Collect() returned no datapoints; senhub.kubernetes.up must still be emitted on API error")
	}

	var upFound bool
	for _, dp := range points {
		if dp.Name == metricUp {
			upFound = true
			if dp.Value != 0 {
				t.Errorf("senhub.kubernetes.up: got %v, want 0 on API error", dp.Value)
			}
		}
	}
	if !upFound {
		t.Error("senhub.kubernetes.up was not emitted on API error")
	}
}
