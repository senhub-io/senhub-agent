package otlpreceiver

import (
	"errors"
	"net"
	"testing"
	"time"
)

func mustCIDRs(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	var out []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", c, err)
		}
		out = append(out, n)
	}
	return out
}

func TestIngressGuard_NilWhenNothingConfigured(t *testing.T) {
	if g := newIngressGuard(receiverConfig{}); g != nil {
		t.Fatal("expected nil guard with no protections configured")
	}
	var g *ingressGuard
	if err := g.allow("10.0.0.1:1234", ""); err != nil {
		t.Fatalf("nil guard must allow everything, got %v", err)
	}
}

func TestIngressGuard_BearerToken(t *testing.T) {
	g := newIngressGuard(receiverConfig{BearerToken: "s3cret"})

	cases := []struct {
		name   string
		header string
		want   error
	}{
		{"valid", "Bearer s3cret", nil},
		{"missing", "", errUnauthorized},
		{"wrong token", "Bearer nope", errUnauthorized},
		{"wrong scheme", "Basic s3cret", errUnauthorized},
		{"token without scheme", "s3cret", errUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := g.allow("127.0.0.1:5", c.header)
			if c.want == nil {
				if err != nil {
					t.Errorf("allow() = %v, want nil", err)
				}
			} else if !errors.Is(err, c.want) {
				t.Errorf("allow() = %v, want %v", err, c.want)
			}
		})
	}
}

func TestIngressGuard_CIDRAllowList(t *testing.T) {
	g := newIngressGuard(receiverConfig{
		AllowedCIDRs: mustCIDRs(t, "10.0.0.0/8", "2001:db8::/32"),
	})

	cases := []struct {
		remote string
		want   error
	}{
		{"10.1.2.3:4317", nil},
		{"[2001:db8::1]:4317", nil},
		{"192.168.1.1:4317", errForbidden},
		{"not-an-ip", errForbidden},
	}
	for _, c := range cases {
		err := g.allow(c.remote, "")
		if c.want == nil {
			if err != nil {
				t.Errorf("allow(%q) = %v, want nil", c.remote, err)
			}
		} else if !errors.Is(err, c.want) {
			t.Errorf("allow(%q) = %v, want %v", c.remote, err, c.want)
		}
	}
}

func TestIngressGuard_ChecksComposeTokenBeforeCIDR(t *testing.T) {
	g := newIngressGuard(receiverConfig{
		BearerToken:  "s3cret",
		AllowedCIDRs: mustCIDRs(t, "10.0.0.0/8"),
	})
	// Wrong token from a denied source: identity error wins.
	if err := g.allow("192.168.1.1:1", "Bearer nope"); !errors.Is(err, errUnauthorized) {
		t.Errorf("want errUnauthorized first, got %v", err)
	}
	// Good token, denied source.
	if err := g.allow("192.168.1.1:1", "Bearer s3cret"); !errors.Is(err, errForbidden) {
		t.Errorf("want errForbidden, got %v", err)
	}
	// Good token, allowed source.
	if err := g.allow("10.0.0.1:1", "Bearer s3cret"); err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestTokenBucket_BurstThenDenyThenRefill(t *testing.T) {
	clock := time.Unix(1000, 0)
	b := newTokenBucket(10, 3) // 10 rps, burst 3
	b.now = func() time.Time { return clock }

	for i := 0; i < 3; i++ {
		if !b.take() {
			t.Fatalf("take %d within burst must succeed", i)
		}
	}
	if b.take() {
		t.Fatal("take beyond burst must fail")
	}

	// 200ms at 10 rps refills 2 tokens.
	clock = clock.Add(200 * time.Millisecond)
	if !b.take() || !b.take() {
		t.Fatal("refilled tokens must be usable")
	}
	if b.take() {
		t.Fatal("third take after partial refill must fail")
	}

	// A long idle period caps at burst, not unbounded.
	clock = clock.Add(time.Hour)
	granted := 0
	for b.take() {
		granted++
	}
	if granted != 3 {
		t.Fatalf("after long idle, granted = %d, want burst cap 3", granted)
	}
}

func TestParseReceiverConfig_GuardParams(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{
		"bearer_token":     "tok",
		"allowed_cidrs":    []interface{}{"10.0.0.0/8", "192.168.0.0/16"},
		"rate_limit_rps":   50,
		"rate_limit_burst": 200,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BearerToken != "tok" || len(cfg.AllowedCIDRs) != 2 ||
		cfg.RateLimitRPS != 50 || cfg.RateLimitBurst != 200 {
		t.Errorf("parsed = %+v", cfg)
	}
}

func TestParseReceiverConfig_GuardParamErrors(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"bad CIDR":          {"allowed_cidrs": []interface{}{"10.0.0.0"}},
		"negative rps":      {"rate_limit_rps": -1},
		"burst without rps": {"rate_limit_burst": 10},
		"burst less than 1": {"rate_limit_rps": 10, "rate_limit_burst": 0},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReceiverConfig(params); err == nil {
				t.Fatal("expected a configuration error, got nil")
			}
		})
	}
}

func TestParseReceiverConfig_DefaultBurstFromRPS(t *testing.T) {
	cfg, err := parseReceiverConfig(map[string]interface{}{"rate_limit_rps": 25})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RateLimitBurst != 50 {
		t.Errorf("default burst = %d, want 50 (2x rps)", cfg.RateLimitBurst)
	}
}
