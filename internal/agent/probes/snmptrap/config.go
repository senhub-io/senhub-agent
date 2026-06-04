package snmptrap

import (
	"fmt"
	"strings"
)

const (
	// ProbeType is the canonical, stable type identifier. It is part of
	// licence claims and config files in the wild — renaming it is a
	// breaking change.
	ProbeType = "snmp_trap"

	defaultBindAddress = "0.0.0.0:162"
	defaultVersion     = "v2c"
)

// v3User is one USM (User-based Security Model) credential used to
// authenticate and decrypt SNMP v3 traps.
type v3User struct {
	Username     string
	AuthProtocol string // "", "MD5", "SHA", "SHA224", "SHA256", "SHA384", "SHA512"
	AuthPassword string
	PrivProtocol string // "", "DES", "AES", "AES192", "AES256"
	PrivPassword string
}

// receiverConfig is the parsed, validated configuration of the trap
// receiver probe.
type receiverConfig struct {
	// BindAddress is the UDP listen address (host:port). Default
	// 0.0.0.0:162 — the well-known SNMP trap port (privileged; needs
	// root or CAP_NET_BIND_SERVICE, see issue #223).
	BindAddress string

	// Version is "v2c" (community-based) or "v3" (USM).
	Version string

	// Community authenticates v2c traps. Empty accepts any community
	// (gosnmp does not enforce it on receive); operators should set it.
	Community string

	// V3Users are the USM credentials for v3 traps.
	V3Users []v3User
}

func parseConfig(config map[string]interface{}) (receiverConfig, error) {
	cfg := receiverConfig{
		BindAddress: defaultBindAddress,
		Version:     defaultVersion,
	}

	if v, ok := config["bind_address"].(string); ok && v != "" {
		cfg.BindAddress = v
	}
	if v, ok := config["version"].(string); ok && v != "" {
		cfg.Version = strings.ToLower(v)
	}
	if cfg.Version != "v2c" && cfg.Version != "v3" {
		return cfg, fmt.Errorf("snmp_trap: version must be \"v2c\" or \"v3\", got %q", cfg.Version)
	}

	if v, ok := config["community"].(string); ok {
		cfg.Community = v
	}

	users, err := parseV3Users(config["v3"])
	if err != nil {
		return cfg, err
	}
	cfg.V3Users = users

	if cfg.Version == "v3" && len(cfg.V3Users) == 0 {
		return cfg, fmt.Errorf("snmp_trap: version v3 requires at least one user under v3.users")
	}

	return cfg, nil
}

// parseV3Users extracts the v3.users list from the raw config. The shape
// is `v3: { users: [ {username, auth_protocol, auth_password,
// priv_protocol, priv_password}, ... ] }`.
func parseV3Users(raw interface{}) ([]v3User, error) {
	v3, ok := raw.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	usersRaw, ok := v3["users"].([]interface{})
	if !ok {
		return nil, nil
	}
	out := make([]v3User, 0, len(usersRaw))
	for i, ur := range usersRaw {
		um, ok := ur.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("snmp_trap: v3.users[%d] must be a mapping", i)
		}
		u := v3User{
			Username:     stringField(um, "username"),
			AuthProtocol: strings.ToUpper(stringField(um, "auth_protocol")),
			AuthPassword: stringField(um, "auth_password"),
			PrivProtocol: strings.ToUpper(stringField(um, "priv_protocol")),
			PrivPassword: stringField(um, "priv_password"),
		}
		if u.Username == "" {
			return nil, fmt.Errorf("snmp_trap: v3.users[%d] requires a username", i)
		}
		out = append(out, u)
	}
	return out, nil
}

func stringField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
