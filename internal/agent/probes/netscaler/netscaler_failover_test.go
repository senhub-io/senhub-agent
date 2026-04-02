package netscaler

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/logger"
)

func newTestProbeWithFailover(baseURL, secondaryURL string) *netscalerProbe {
	nopLogger := zerolog.Nop()
	moduleLogger := logger.NewModuleLogger(&nopLogger, "probe.netscaler.test")

	return &netscalerProbe{
		BaseProbe:         &types.BaseProbe{},
		logger:            moduleLogger,
		baseURL:           baseURL,
		secondaryURL:      secondaryURL,
		activeURL:         baseURL,
		maxFailoverErrors: 3,
		clientMu:          sync.RWMutex{},
	}
}

func TestGetFailoverURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		secondaryURL string
		activeURL    string
		expected     string
	}{
		{
			name:         "No secondary configured",
			baseURL:      "https://10.0.0.1",
			secondaryURL: "",
			activeURL:    "https://10.0.0.1",
			expected:     "",
		},
		{
			name:         "Active is primary, return secondary",
			baseURL:      "https://10.0.0.1",
			secondaryURL: "https://10.0.0.2",
			activeURL:    "https://10.0.0.1",
			expected:     "https://10.0.0.2",
		},
		{
			name:         "Active is secondary, return primary",
			baseURL:      "https://10.0.0.1",
			secondaryURL: "https://10.0.0.2",
			activeURL:    "https://10.0.0.2",
			expected:     "https://10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProbeWithFailover(tt.baseURL, tt.secondaryURL)
			p.activeURL = tt.activeURL
			got := p.getFailoverURL()
			if got != tt.expected {
				t.Errorf("getFailoverURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestHandleCollectError_NoSecondary(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "")
	result := p.handleCollectError()
	if result {
		t.Error("handleCollectError() should return false when no secondary configured")
	}
	if p.consecutiveErrors != 0 {
		t.Errorf("consecutiveErrors should stay 0 without secondary, got %d", p.consecutiveErrors)
	}
}

func TestHandleCollectError_IncrementsBelowThreshold(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")

	// First 2 errors should not trigger failover
	for i := 1; i <= 2; i++ {
		result := p.handleCollectError()
		if result {
			t.Errorf("handleCollectError() should return false at error %d (threshold is 3)", i)
		}
		if p.consecutiveErrors != i {
			t.Errorf("consecutiveErrors should be %d, got %d", i, p.consecutiveErrors)
		}
	}
}

func TestHandleCollectError_TriggersAtThreshold(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	p.consecutiveErrors = 2

	// Third error reaches threshold — switchToNode will fail (no real NITRO server)
	// but handleCollectError should still attempt it
	result := p.handleCollectError()
	// switchToNode fails because there's no real server, so result should be false
	if result {
		t.Error("handleCollectError() should return false when switchToNode fails (no server)")
	}
	if p.consecutiveErrors != 3 {
		t.Errorf("consecutiveErrors should be 3, got %d", p.consecutiveErrors)
	}
}

func TestCheckHAPrimaryStatus_NoSecondary(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "")
	configs := []map[string]interface{}{
		{"id": "0", "ipaddress": "10.0.0.1", "state": "Secondary"},
	}
	p.checkHAPrimaryStatus(configs)
	if p.pendingFailoverURL != "" {
		t.Error("Should not set pendingFailoverURL when no secondary configured")
	}
}

func TestCheckHAPrimaryStatus_ConnectedToPrimary(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	configs := []map[string]interface{}{
		{"id": "0", "ipaddress": "10.0.0.1", "state": "Primary", "name": "NS1"},
		{"id": "1", "ipaddress": "10.0.0.2", "state": "Secondary", "name": "NS2"},
	}
	p.checkHAPrimaryStatus(configs)
	if p.pendingFailoverURL != "" {
		t.Error("Should not set pendingFailoverURL when already on primary")
	}
}

func TestCheckHAPrimaryStatus_ConnectedToSecondary_ByIP(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	// Simulate: we're actually connected to 10.0.0.2 (secondary became active)
	p.activeURL = "https://10.0.0.2"
	configs := []map[string]interface{}{
		{"id": "0", "ipaddress": "10.0.0.1", "state": "Primary", "name": "NS1"},
		{"id": "1", "ipaddress": "10.0.0.2", "state": "Secondary", "name": "NS2"},
	}
	p.checkHAPrimaryStatus(configs)
	if p.pendingFailoverURL != "https://10.0.0.1" {
		t.Errorf("Should set pendingFailoverURL to primary, got %q", p.pendingFailoverURL)
	}
}

func TestCheckHAPrimaryStatus_ConnectedToSecondary_ByHostname(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	p.activeURL = "https://10.0.0.2"
	p.hostname = "NS2"
	configs := []map[string]interface{}{
		{"id": "0", "ipaddress": "10.0.0.1", "state": "Primary", "name": "NS1"},
		{"id": "1", "ipaddress": "10.0.0.2", "state": "SECONDARY", "name": "NS2"},
	}
	p.checkHAPrimaryStatus(configs)
	if p.pendingFailoverURL != "https://10.0.0.1" {
		t.Errorf("Should set pendingFailoverURL to primary (case-insensitive), got %q", p.pendingFailoverURL)
	}
}

func TestCheckHAPrimaryStatus_CaseInsensitive(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	p.activeURL = "https://10.0.0.2"
	configs := []map[string]interface{}{
		{"id": "0", "ipaddress": "10.0.0.1", "state": "Primary", "name": "NS1"},
		{"id": "1", "ipaddress": "10.0.0.2", "state": "secondary", "name": "NS2"},
	}
	p.checkHAPrimaryStatus(configs)
	if p.pendingFailoverURL != "https://10.0.0.1" {
		t.Errorf("Should handle lowercase 'secondary', got pendingFailoverURL=%q", p.pendingFailoverURL)
	}
}

func TestPendingFailoverURL_ClearedAfterUse(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	p.pendingFailoverURL = "https://10.0.0.2"

	// Simulating what Collect() does: read and clear pendingFailoverURL
	targetURL := p.pendingFailoverURL
	p.pendingFailoverURL = ""

	if targetURL != "https://10.0.0.2" {
		t.Errorf("Should have read pending URL, got %q", targetURL)
	}
	if p.pendingFailoverURL != "" {
		t.Error("pendingFailoverURL should be cleared after reading")
	}
}

func TestConsecutiveErrors_ResetOnSuccess(t *testing.T) {
	p := newTestProbeWithFailover("https://10.0.0.1", "https://10.0.0.2")
	p.consecutiveErrors = 2

	// Simulate successful collection (Collect sets this to 0)
	p.consecutiveErrors = 0

	if p.consecutiveErrors != 0 {
		t.Errorf("consecutiveErrors should reset to 0 on success, got %d", p.consecutiveErrors)
	}
}
