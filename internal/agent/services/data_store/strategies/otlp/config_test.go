package otlp

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfig_DefaultsApplied(t *testing.T) {
	cfg, err := ParseConfig(map[string]interface{}{
		"endpoint": "otlp.example.com:4317",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "otlp.example.com:4317" {
		t.Errorf("Endpoint=%q", cfg.Endpoint)
	}
	if cfg.Compression != DefaultCompression {
		t.Errorf("Compression=%q, want %q", cfg.Compression, DefaultCompression)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Errorf("Timeout=%v, want %v", cfg.Timeout, DefaultTimeout)
	}
	if !cfg.TLS.Enabled {
		t.Errorf("TLS.Enabled should default to true")
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.Interval != DefaultMetricsInterval {
		t.Errorf("Metrics defaults wrong: %+v", cfg.Metrics)
	}
	if !cfg.Logs.Enabled || cfg.Logs.BatchSize != DefaultLogsBatchSize {
		t.Errorf("Logs defaults wrong: %+v", cfg.Logs)
	}
	if !cfg.Retry.Enabled {
		t.Errorf("Retry should default to enabled")
	}
	if cfg.Resource.ServiceName != DefaultServiceName {
		t.Errorf("Resource.ServiceName=%q, want %q", cfg.Resource.ServiceName, DefaultServiceName)
	}
}

func TestParseConfig_RejectsMissingEndpoint(t *testing.T) {
	cfg := defaultConfig()
	cfg.Endpoint = ""
	_, err := ParseConfig(map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error doesn't mention endpoint: %v", err)
	}
}

func TestParseConfig_RejectsBadCompression(t *testing.T) {
	_, err := ParseConfig(map[string]interface{}{
		"endpoint":    "x:4317",
		"compression": "snappy",
	})
	if err == nil {
		t.Fatal("expected error for unsupported compression")
	}
	if !strings.Contains(err.Error(), "compression") {
		t.Errorf("error doesn't mention compression: %v", err)
	}
}

func TestParseConfig_RejectsBadTimeout(t *testing.T) {
	_, err := ParseConfig(map[string]interface{}{
		"endpoint": "x:4317",
		"timeout":  "not-a-duration",
	})
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}

func TestParseConfig_RejectsHalfMTLS(t *testing.T) {
	// cert_file without key_file is invalid.
	_, err := ParseConfig(map[string]interface{}{
		"endpoint": "x:4317",
		"tls": map[string]interface{}{
			"enabled":   true,
			"cert_file": "/some/cert.pem",
		},
	})
	if err == nil {
		t.Fatal("expected error for half-mTLS")
	}
	if !strings.Contains(err.Error(), "cert_file") && !strings.Contains(err.Error(), "key_file") {
		t.Errorf("error doesn't mention cert/key file: %v", err)
	}
}

func TestParseConfig_RetryAndSignals(t *testing.T) {
	cfg, err := ParseConfig(map[string]interface{}{
		"endpoint": "x:4317",
		"retry": map[string]interface{}{
			"enabled":          false,
			"initial_interval": "1s",
			"max_interval":     "10s",
			"max_elapsed_time": "30s",
		},
		"signals": map[string]interface{}{
			"metrics": map[string]interface{}{
				"enabled":     true,
				"interval":    "60s",
				"temporality": "delta",
			},
			"logs": map[string]interface{}{
				"enabled":       false,
				"batch_size":    500,
				"batch_timeout": "2s",
				"buffer_size":   2048,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Retry.Enabled {
		t.Errorf("Retry.Enabled should be false")
	}
	if cfg.Retry.InitialInterval != time.Second {
		t.Errorf("InitialInterval=%v", cfg.Retry.InitialInterval)
	}
	if cfg.Metrics.Interval != 60*time.Second {
		t.Errorf("Metrics.Interval=%v", cfg.Metrics.Interval)
	}
	if cfg.Metrics.Temporality != "delta" {
		t.Errorf("Temporality=%q", cfg.Metrics.Temporality)
	}
	if cfg.Logs.Enabled {
		t.Errorf("Logs.Enabled should be false")
	}
	if cfg.Logs.BatchSize != 500 {
		t.Errorf("Logs.BatchSize=%d", cfg.Logs.BatchSize)
	}
	if cfg.Logs.BufferSize != 2048 {
		t.Errorf("Logs.BufferSize=%d", cfg.Logs.BufferSize)
	}
}

func TestParseConfig_BadTemporality(t *testing.T) {
	_, err := ParseConfig(map[string]interface{}{
		"endpoint": "x:4317",
		"signals": map[string]interface{}{
			"metrics": map[string]interface{}{
				"temporality": "wrong",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "temporality") {
		t.Fatalf("expected temporality error, got: %v", err)
	}
}

func TestParseConfig_Resource(t *testing.T) {
	cfg, err := ParseConfig(map[string]interface{}{
		"endpoint": "x:4317",
		"resource": map[string]interface{}{
			"service.name":           "agent-paris",
			"service.instance.id":    "abc12345",
			"deployment.environment": "prod",
			"k8s.cluster.name":       "edge-01",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Resource.ServiceName != "agent-paris" {
		t.Errorf("ServiceName=%q", cfg.Resource.ServiceName)
	}
	if cfg.Resource.ServiceInstance != "abc12345" {
		t.Errorf("ServiceInstance=%q", cfg.Resource.ServiceInstance)
	}
	if cfg.Resource.Environment != "prod" {
		t.Errorf("Environment=%q", cfg.Resource.Environment)
	}
	if cfg.Resource.Extra["k8s.cluster.name"] != "edge-01" {
		t.Errorf("Extra missing k8s.cluster.name: %v", cfg.Resource.Extra)
	}
}

func TestParseConfig_HeadersAndYAMLMaps(t *testing.T) {
	// Simulate the case where YAML decodes into map[interface{}]interface{}
	// (gopkg.in/yaml.v2 default behavior). The parser must handle it.
	cfg, err := ParseConfig(map[string]interface{}{
		"endpoint": "x:4317",
		"headers": map[interface{}]interface{}{
			"Authorization": "Bearer XYZ",
			"X-Tenant":      "acme",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Headers["Authorization"] != "Bearer XYZ" {
		t.Errorf("Authorization header missing: %+v", cfg.Headers)
	}
	if cfg.Headers["X-Tenant"] != "acme" {
		t.Errorf("X-Tenant header missing: %+v", cfg.Headers)
	}
}
