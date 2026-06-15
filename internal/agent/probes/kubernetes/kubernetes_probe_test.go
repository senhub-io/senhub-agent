package kubernetes

import (
	"testing"
	"time"
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
