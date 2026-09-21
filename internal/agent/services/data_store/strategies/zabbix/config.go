// Package zabbix pushes the collected metrics to a Zabbix server or proxy
// as a native active agent: the agent connects out to port 10051, asks for
// the items the server wants for this host, and sends their values in
// batches. An unknown host triggers the server's autoregistration, so a
// fleet appears in Zabbix without anyone creating hosts by hand.
//
// The design and the protocol references are in
// docs/developer-guide/zabbix/INTEGRATION-STUDY.md. The protocol is
// implemented from its public documentation; no Zabbix source is used.
package zabbix

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
)

const (
	defaultPort              = "10051"
	defaultInterval          = 60 * time.Second
	defaultRefreshInterval   = 120 * time.Second
	defaultHeartbeatInterval = 60 * time.Second
	defaultTimeout           = 10 * time.Second
	defaultKeyPrefix         = "senhub"
	defaultHostMetadata      = "senhub-agent"
)

// TLSConfig is certificate-based encryption for the outbound connection.
// Zabbix also offers pre-shared keys, which Go's crypto/tls cannot do
// (golang/go#6379): a host that must be encrypted uses a certificate, and
// its autoregistration, which Zabbix only encrypts with PSK, stays in
// clear or goes through a local proxy.
type TLSConfig struct {
	Enabled            bool
	CAFile             string
	CertFile           string
	KeyFile            string
	ServerName         string
	InsecureSkipVerify bool
}

// parseServers reads the 'server' parameter, which holds one address or
// several separated by commas. Several is how a proxy group is named:
// any member answers, and the one that does redirects the agent to
// whichever member currently holds this host.
func parseServers(raw string) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		addr := strings.TrimSpace(part)
		if addr == "" {
			continue
		}
		if _, _, splitErr := net.SplitHostPort(addr); splitErr != nil {
			if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") {
				return nil, fmt.Errorf("zabbix: 'server' %q is not host:port", addr)
			}
			addr = net.JoinHostPort(strings.Trim(addr, "[]"), defaultPort)
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("zabbix: 'server' is required (host:port of the Zabbix server or proxy; several separated by commas name a proxy group)")
	}
	return out, nil
}

// addresses lists the server addresses, tolerating a Config assembled by
// hand with only Server set.
func (c Config) addresses() []string {
	if len(c.Servers) > 0 {
		return c.Servers
	}
	if c.Server != "" {
		return []string{c.Server}
	}
	return nil
}

// Config is the parsed strategy configuration.
type Config struct {
	// Server is the first address of Servers, kept for the messages and
	// for the TLS name when the operator pinned neither.
	Server string
	// Servers is every address the agent may talk to, in order. A proxy
	// group is written as several addresses separated by commas, the way
	// Zabbix's own agent takes several ServerActive entries: the agent
	// talks to the first that answers, and the group redirects it to
	// whichever member currently holds this host.
	Servers []string
	// Hostname is the name this host registers under. Defaults to the
	// machine's host name.
	Hostname string
	// HostMetadata travels with every check-list request; the server's
	// autoregistration action matches on it to pick host groups and
	// templates.
	HostMetadata string
	// Interval is the push cadence of the collected values.
	Interval time.Duration
	// RefreshInterval is how often the item list is asked again.
	RefreshInterval time.Duration
	// HeartbeatInterval is the cadence of the active-check heartbeat;
	// the server declares the host unavailable after twice that.
	HeartbeatInterval time.Duration
	// Timeout bounds one connection, request and reply.
	Timeout time.Duration
	// KeyPrefix is the first segment of every item key.
	KeyPrefix string
	TLS       TLSConfig
	Passive   PassiveConfig
}

// PassiveConfig is the optional listener the server polls, the way it
// polls a classic Zabbix agent: it answers agent.ping so the host's
// availability icon turns green, and serves the same item keys as the
// active push for an operator who prefers passive items.
type PassiveConfig struct {
	Enabled     bool
	BindAddress string
	Port        int
	// Allow lists the addresses (IP or CIDR) allowed to poll. Empty
	// means the addresses the configured server resolves to.
	Allow []string
}

const (
	defaultPassiveBind = "0.0.0.0"
	defaultPassivePort = 10050
)

var keyPrefixPattern = regexp.MustCompile(`^[0-9a-zA-Z_.-]+$`)

// ParseConfig reads the strategy parameters and applies the defaults.
func ParseConfig(params configuration.StorageConfigParams) (Config, error) {
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	cfg := Config{
		Hostname:          host,
		HostMetadata:      defaultHostMetadata,
		Interval:          defaultInterval,
		RefreshInterval:   defaultRefreshInterval,
		HeartbeatInterval: defaultHeartbeatInterval,
		Timeout:           defaultTimeout,
		KeyPrefix:         defaultKeyPrefix,
	}

	raw, _ := params["server"].(string)
	servers, serverErr := parseServers(raw)
	if serverErr != nil {
		return cfg, serverErr
	}
	cfg.Servers = servers
	cfg.Server = servers[0]

	if v, ok := params["hostname"]; ok {
		s, isStr := v.(string)
		if !isStr || strings.TrimSpace(s) == "" {
			return cfg, fmt.Errorf("zabbix: 'hostname' must be a non-empty string")
		}
		cfg.Hostname = strings.TrimSpace(s)
	}
	if cfg.Hostname == "" {
		return cfg, fmt.Errorf("zabbix: 'hostname' is required when the machine's host name cannot be read")
	}

	if v, ok := params["host_metadata"]; ok {
		s, isStr := v.(string)
		if !isStr {
			return cfg, fmt.Errorf("zabbix: 'host_metadata' must be a string")
		}
		if len(s) > 2034 {
			return cfg, fmt.Errorf("zabbix: 'host_metadata' is limited to 2034 bytes by Zabbix, got %d", len(s))
		}
		cfg.HostMetadata = s
	}

	if v, ok := params["key_prefix"]; ok {
		s, isStr := v.(string)
		if !isStr || !keyPrefixPattern.MatchString(s) {
			return cfg, fmt.Errorf("zabbix: 'key_prefix' must contain only letters, digits, '_', '-' and '.'")
		}
		cfg.KeyPrefix = s
	}

	for _, d := range []struct {
		key  string
		dest *time.Duration
	}{
		{"interval", &cfg.Interval},
		{"refresh_interval", &cfg.RefreshInterval},
		{"heartbeat_interval", &cfg.HeartbeatInterval},
		{"timeout", &cfg.Timeout},
	} {
		v, ok := params[d.key]
		if !ok {
			continue
		}
		dur, err := parseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("zabbix: '%s': %w", d.key, err)
		}
		if dur <= 0 {
			return cfg, fmt.Errorf("zabbix: '%s' must be positive", d.key)
		}
		*d.dest = dur
	}
	if cfg.HeartbeatInterval < 1*time.Second {
		return cfg, fmt.Errorf("zabbix: 'heartbeat_interval' must be at least 1s")
	}

	if raw, ok := params["tls"]; ok {
		block, isMap := raw.(map[string]interface{})
		if !isMap {
			return cfg, fmt.Errorf("zabbix: 'tls' must be a block")
		}
		tlsCfg, err := parseTLS(block)
		if err != nil {
			return cfg, err
		}
		cfg.TLS = tlsCfg
	}

	cfg.Passive = PassiveConfig{BindAddress: defaultPassiveBind, Port: defaultPassivePort}
	if raw, ok := params["passive"]; ok {
		block, isMap := raw.(map[string]interface{})
		if !isMap {
			return cfg, fmt.Errorf("zabbix: 'passive' must be a block")
		}
		pc, err := parsePassive(block, cfg.Passive)
		if err != nil {
			return cfg, err
		}
		cfg.Passive = pc
	}
	return cfg, nil
}

func parsePassive(block map[string]interface{}, cfg PassiveConfig) (PassiveConfig, error) {
	if v, ok := block["enabled"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return cfg, fmt.Errorf("zabbix: 'passive.enabled' must be true or false")
		}
		cfg.Enabled = b
	}
	if v, ok := block["bind_address"]; ok {
		s, isStr := v.(string)
		if !isStr || net.ParseIP(strings.TrimSpace(s)) == nil {
			return cfg, fmt.Errorf("zabbix: 'passive.bind_address' must be an IP address")
		}
		cfg.BindAddress = strings.TrimSpace(s)
	}
	if v, ok := block["port"]; ok {
		port, err := parsePort(v)
		if err != nil {
			return cfg, fmt.Errorf("zabbix: 'passive.port': %w", err)
		}
		cfg.Port = port
	}
	if v, ok := block["allow"]; ok {
		list, isList := v.([]interface{})
		if !isList {
			return cfg, fmt.Errorf("zabbix: 'passive.allow' must be a list of IP addresses or CIDR ranges")
		}
		for _, e := range list {
			s, isStr := e.(string)
			if !isStr {
				return cfg, fmt.Errorf("zabbix: 'passive.allow' must be a list of IP addresses or CIDR ranges")
			}
			s = strings.TrimSpace(s)
			if _, _, err := net.ParseCIDR(s); err != nil && net.ParseIP(s) == nil {
				return cfg, fmt.Errorf("zabbix: 'passive.allow' entry %q is neither an IP address nor a CIDR range", s)
			}
			cfg.Allow = append(cfg.Allow, s)
		}
	}
	return cfg, nil
}

func parsePort(v interface{}) (int, error) {
	var port int
	switch t := v.(type) {
	case int:
		port = t
	case int64:
		port = int(t)
	case float64:
		port = int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, fmt.Errorf("%q is not a port number", t)
		}
		port = n
	default:
		return 0, fmt.Errorf("expected a port number, got %T", v)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%d is outside 1-65535", port)
	}
	return port, nil
}

func parseTLS(block map[string]interface{}) (TLSConfig, error) {
	var cfg TLSConfig
	if v, ok := block["enabled"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return cfg, fmt.Errorf("zabbix: 'tls.enabled' must be true or false")
		}
		cfg.Enabled = b
	}
	for _, s := range []struct {
		key  string
		dest *string
	}{
		{"ca_file", &cfg.CAFile},
		{"cert_file", &cfg.CertFile},
		{"key_file", &cfg.KeyFile},
		{"server_name", &cfg.ServerName},
	} {
		v, ok := block[s.key]
		if !ok {
			continue
		}
		str, isStr := v.(string)
		if !isStr {
			return cfg, fmt.Errorf("zabbix: 'tls.%s' must be a string", s.key)
		}
		*s.dest = strings.TrimSpace(str)
	}
	if v, ok := block["insecure_skip_verify"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return cfg, fmt.Errorf("zabbix: 'tls.insecure_skip_verify' must be true or false")
		}
		cfg.InsecureSkipVerify = b
	}
	if (cfg.CertFile == "") != (cfg.KeyFile == "") {
		return cfg, fmt.Errorf("zabbix: 'tls.cert_file' and 'tls.key_file' go together")
	}
	return cfg, nil
}

// parseDuration reads "30s", a number of seconds, or an integer.
func parseDuration(v interface{}) (time.Duration, error) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if d, err := time.ParseDuration(s); err == nil {
			return d, nil
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return time.Duration(n * float64(time.Second)), nil
		}
		return 0, fmt.Errorf("%q is not a duration (use 30s, 2m)", t)
	case int:
		return time.Duration(t) * time.Second, nil
	case int64:
		return time.Duration(t) * time.Second, nil
	case float64:
		return time.Duration(t * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("expected a duration, got %T", v)
	}
}
