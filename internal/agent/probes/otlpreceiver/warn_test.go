package otlpreceiver

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/logger"
)

func TestWarnIfUnprotectedWildcard(t *testing.T) {
	const wildcardWarning = "bound to ALL interfaces without authentication"

	tests := []struct {
		name     string
		cfg      map[string]interface{}
		wantWarn bool
	}{
		{"wildcard, nothing configured", map[string]interface{}{"address": "0.0.0.0:4317"}, true},
		{"wildcard, token only", map[string]interface{}{"address": "0.0.0.0:4317", "bearer_token": "s3cret"}, false},
		{"wildcard, cidrs only", map[string]interface{}{"address": "0.0.0.0:4317", "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, false},
		{"wildcard, token and cidrs", map[string]interface{}{"address": "0.0.0.0:4317", "bearer_token": "s3cret", "allowed_cidrs": []interface{}{"10.0.0.0/8"}}, false},
		{"loopback, nothing configured", map[string]interface{}{"address": "127.0.0.1:4317"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			zlog := zerolog.New(&buf)
			p, err := NewOTLPReceiverProbe(tt.cfg, (*logger.Logger)(&zlog))
			if err != nil {
				t.Fatalf("NewOTLPReceiverProbe: %v", err)
			}
			p.(*OTLPReceiverProbe).warnIfUnprotectedWildcard()
			if got := strings.Contains(buf.String(), wildcardWarning); got != tt.wantWarn {
				t.Errorf("warning present = %v, want %v (log: %q)", got, tt.wantWarn, buf.String())
			}
		})
	}
}

func TestGuardFromConfig_TokenAndCIDRsBothEnforced(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{
		"address":       "0.0.0.0:4317",
		"bearer_token":  "s3cret",
		"allowed_cidrs": []interface{}{"10.0.0.0/8"},
	})
	if err != nil {
		t.Fatalf("parseReceiverConfig: %v", err)
	}
	g := newIngressGuard(cfg)
	if g == nil {
		t.Fatal("guard is nil although bearer_token and allowed_cidrs are set")
	}

	if err := g.allow("10.1.2.3:5000", ""); !errors.Is(err, errUnauthorized) {
		t.Errorf("allowed CIDR without token: %v, want errUnauthorized", err)
	}
	if err := g.allow("192.0.2.9:5000", "Bearer s3cret"); !errors.Is(err, errForbidden) {
		t.Errorf("token from non-allowed address: %v, want errForbidden", err)
	}
	if err := g.allow("10.1.2.3:5000", "Bearer s3cret"); err != nil {
		t.Errorf("valid request: %v", err)
	}
}
