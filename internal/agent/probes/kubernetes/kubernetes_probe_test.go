package kubernetes

import (
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/types"
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
