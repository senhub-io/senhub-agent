package snmptrap

import (
	"fmt"
	"strings"

	"senhub-agent.go/internal/agent/probes/types"
)

const (
	// ProbeType is the canonical, stable type identifier. It is part of
	// licence claims and config files in the wild — renaming it is a
	// breaking change.
	ProbeType = "snmp_trap"

	// The listen default is loopback-only (#278): receiving traps
	// from network devices requires an explicit `bind_address`
	// opt-in, which is also the only configuration in which the
	// probe is useful — a silent bind-all default mostly exposed an
	// unauthenticated UDP listener on hosts that never configured a
	// sender.
	defaultBindAddress = "127.0.0.1:162"
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

	// Community authenticates v1/v2c traps: received datagrams whose
	// community does not match are rejected and counted
	// (senhub.snmp_trap.rejected_community). An empty value accepts
	// any community — operators should always set it (#263).
	Community string

	// V3Users are the USM credentials for v3 traps.
	V3Users []v3User

	// MibPaths are local directories or files of operator-supplied MIB
	// modules, loaded at startup to resolve trap and varbind OIDs to
	// names (never fetched over the network). Empty = numeric OIDs only
	// (plus the built-in generic-trap table).
	MibPaths []string
}

func parseConfig(config map[string]interface{}) (receiverConfig, error) {
	cfg := receiverConfig{
		BindAddress: defaultBindAddress,
		Version:     defaultVersion,
	}

	if v, ok := types.StringParam(config, "bind_address"); ok && v != "" {
		cfg.BindAddress = v
	}
	if v, ok := types.StringParam(config, "version"); ok && v != "" {
		cfg.Version = strings.ToLower(v)
	}
	if cfg.Version != "v2c" && cfg.Version != "v3" {
		return cfg, fmt.Errorf("snmp_trap: version must be \"v2c\" or \"v3\", got %q", cfg.Version)
	}

	if v, ok := types.StringParam(config, "community"); ok {
		cfg.Community = v
	}

	cfg.MibPaths, _ = types.StringSliceParam(config, "mib_paths")

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

// stringField is the "absent means empty" form this file wants, over the
// shared helper rather than over a bare assertion — so a value that
// arrives as something other than a string is handled the same way here
// as everywhere else (#831).
func stringField(m map[string]interface{}, key string) string {
	v, _ := types.StringParam(m, key)
	return v
}
