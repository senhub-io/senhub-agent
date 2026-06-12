package snmptrap

import "testing"

func TestParseConfig_Defaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// #278: the default must stay loopback — receiving traps from the
	// network is an explicit bind_address opt-in.
	if cfg.BindAddress != "127.0.0.1:162" {
		t.Errorf("BindAddress = %q, want 127.0.0.1:162", cfg.BindAddress)
	}
	if cfg.Version != "v2c" {
		t.Errorf("Version = %q, want v2c", cfg.Version)
	}
}

func TestParseConfig_V2c(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"bind_address": "0.0.0.0:16200",
		"version":      "V2C",
		"community":    "public",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.BindAddress != "0.0.0.0:16200" || cfg.Version != "v2c" || cfg.Community != "public" {
		t.Errorf("parsed = %+v", cfg)
	}
}

func TestParseConfig_RejectsBadVersion(t *testing.T) {
	if _, err := parseConfig(map[string]interface{}{"version": "v4"}); err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func TestParseConfig_V3Users(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"version": "v3",
		"v3": map[string]interface{}{
			"users": []interface{}{
				map[string]interface{}{
					"username":      "trap_user",
					"auth_protocol": "sha",
					"auth_password": "authpass123",
					"priv_protocol": "aes",
					"priv_password": "privpass123",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(cfg.V3Users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(cfg.V3Users))
	}
	u := cfg.V3Users[0]
	if u.Username != "trap_user" || u.AuthProtocol != "SHA" || u.PrivProtocol != "AES" {
		t.Errorf("user = %+v (auth/priv should be upper-cased)", u)
	}
}

func TestParseConfig_V3RequiresUser(t *testing.T) {
	if _, err := parseConfig(map[string]interface{}{"version": "v3"}); err == nil {
		t.Fatal("v3 with no users must error")
	}
}

func TestParseConfig_V3UserNeedsUsername(t *testing.T) {
	_, err := parseConfig(map[string]interface{}{
		"version": "v3",
		"v3":      map[string]interface{}{"users": []interface{}{map[string]interface{}{"auth_protocol": "SHA"}}},
	})
	if err == nil {
		t.Fatal("v3 user without username must error")
	}
}
