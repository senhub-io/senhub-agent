package snmppoll

import (
	"github.com/gosnmp/gosnmp"

	"fmt"
	"net"
	"senhub-agent.go/internal/agent/services/governance"
	"senhub-agent.go/internal/agent/services/snmpcore"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
)

const (
	probeType = "snmp_poll"

	defaultPort      uint16 = 161
	defaultCommunity        = "public"
	defaultTimeout          = 5 * time.Second
	defaultRetries          = 2
	defaultInterval         = 60 * time.Second
	// defaultTopologyInterval — the entity rail sweeps topology far slower
	// than metrics (Toise guidance ~5-15 min); see entity_source.go.
	defaultTopologyInterval = 10 * time.Minute

	minPort = 1
	maxPort = 65535

	// Discovery crawl bounds (defaults; overridable in the discovery: block).
	defaultMaxDevices = 200
	defaultMaxHops    = 4

	// metricUp is 1 when the last poll reached the device, 0 otherwise.
	metricUp = "senhub.snmp.up"
	// metricPollDuration is the wall-clock seconds the poll cycle took.
	metricPollDuration = "senhub.snmp.poll.duration"
)

// customMapping is an operator-supplied OID→metric mapping from the YAML
// "custom_mappings:" list. It covers OIDs no built-in module declares
// (vendor objects, long tail) without a built-in profile. A mapping is
// walked as a table column when IndexLabel is set, otherwise fetched as a
// scalar.
type customMapping struct {
	OID        string
	Metric     string
	Kind       metricKind
	IndexLabel string
}

// v3Config holds the USM credentials for SNMPv3 polling. The security
// level (noAuthNoPriv / authNoPriv / authPriv) is derived from which
// protocols are set, like snmp_trap — one source of truth, no separate
// security_level field to contradict it.
type v3Config struct {
	Username     string
	AuthProtocol string // "", "MD5", "SHA", "SHA224", "SHA256", "SHA384", "SHA512"
	AuthPassword string
	PrivProtocol string // "", "DES", "AES", "AES192", "AES256"
	PrivPassword string
}

// config is the resolved, validated snmp_poll probe configuration.
type config struct {
	Target    string
	Port      uint16
	Community string
	// Version is the resolved SNMP protocol version (v2c default).
	Version gosnmp.SnmpVersion
	// V3 carries the USM credentials; non-nil iff Version is Version3.
	V3       *v3Config
	Timeout  time.Duration
	Retries  int
	Interval time.Duration
	// TopologyInterval is the (slow) cadence for the entity-rail topology
	// sweep, independent of the metric Interval. Zero → defaultTopologyInterval.
	TopologyInterval time.Duration

	// MIBs is the ordered list of built-in MIB selectors to poll
	// (e.g. "mib-2", "if-mib").
	MIBs []string
	// Custom holds operator OID mappings beyond the built-in modules.
	Custom []customMapping
	// MibPaths are local directories or files of operator-supplied MIB
	// modules. When set, a custom_mappings entry may omit 'metric': the
	// name is resolved from the MIBs at probe start (never fetched over
	// the network — same posture as snmp_trap).
	MibPaths []string

	// Discovery, when set, enables the SNMP crawl: from the seeds the probe
	// expands the poll set across the LLDP neighbour graph, bounded. nil when
	// the "discovery:" block is absent (single-target mode). The multi-target
	// poll lifecycle that consumes it is a later lot.
	Discovery *discoveryConfig

	// Governance is the operator metadata stamped on the polled device's
	// network.device entity (single-target mode). Empty by default.
	Governance governance.Governance
}

// discoveryConfig is the validated "discovery:" block.
type discoveryConfig struct {
	Seeds        []string         // entry device IPs
	Profile      discoveryProfile // single credential profile for discovered devices
	MaxDevices   int              // hard cap on discovered devices
	MaxHops      int              // BFS depth bound
	AllowedCIDRs []*net.IPNet     // neighbours are crawled only within these
	Interval     time.Duration    // crawl cadence; 0 → topology interval

	// GovernanceRules assign operator metadata to discovered devices by matching
	// their facts (CIDR / vendor / sysName). Empty when no rules are configured.
	GovernanceRules governance.Rules
}

// discoveryProfile is the single SNMP credential applied to every discovered
// device. v2c only for now (snmp_poll does not support v3 yet).
type discoveryProfile struct {
	Version   string
	Community string
}

// parseConfig validates and normalizes the free-form probe params,
// accumulating every problem so an operator fixes a misconfiguration in
// one pass. Lot 1 supports SNMP v2c only (table walks need GETBULK, which
// v1 lacks); "v1"/"v3" are rejected with a clear message rather than
// silently degraded.
func parseConfig(raw map[string]interface{}) (*config, error) {
	var errs []string

	cfg := &config{
		Port:      defaultPort,
		Community: defaultCommunity,
		Timeout:   defaultTimeout,
		Retries:   defaultRetries,
		Interval:  defaultInterval,
	}

	if target, _ := raw["target"].(string); strings.TrimSpace(target) != "" {
		cfg.Target = strings.TrimSpace(target)
	} else {
		errs = append(errs, "target is required")
	}

	cfg.Version = gosnmp.Version2c
	if vs, ok := raw["version"].(string); ok && strings.TrimSpace(vs) != "" {
		v, err := resolveVersion(vs)
		if err != nil {
			errs = append(errs, err.Error())
		} else {
			cfg.Version = v
		}
	}

	v3, err3 := parseV3(raw["v3"], cfg.Version == gosnmp.Version3)
	if err3 != nil {
		errs = append(errs, err3.Error())
	}
	cfg.V3 = v3

	if v, ok := types.IntParam(raw, "port"); ok {
		if v < minPort || v > maxPort {
			errs = append(errs, fmt.Sprintf("port must be between %d and %d", minPort, maxPort))
		} else {
			cfg.Port = uint16(v)
		}
	}

	if c, ok := raw["community"].(string); ok && c != "" {
		cfg.Community = c
	}

	if v, ok := types.IntParam(raw, "retries"); ok {
		if v < 0 {
			errs = append(errs, "retries must be zero or greater")
		} else {
			cfg.Retries = v
		}
	}

	if d, ok, err := durationParam(raw, "timeout"); err != nil {
		errs = append(errs, fmt.Sprintf("timeout: %v", err))
	} else if ok {
		cfg.Timeout = d
	}

	if d, ok, err := durationParam(raw, "interval"); err != nil {
		errs = append(errs, fmt.Sprintf("interval: %v", err))
	} else if ok {
		cfg.Interval = d
	}

	if d, ok, err := durationParam(raw, "topology_interval"); err != nil {
		errs = append(errs, fmt.Sprintf("topology_interval: %v", err))
	} else if ok {
		cfg.TopologyInterval = d
	}

	mibs, err := parseMIBs(raw["mibs"])
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.MIBs = mibs

	cfg.MibPaths = stringSliceOf(raw["mib_paths"])

	custom, err := parseCustomMappings(raw["custom_mappings"], len(cfg.MibPaths) > 0)
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.Custom = custom

	disc, err := parseDiscovery(raw["discovery"])
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.Discovery = disc

	gov, err := governance.Parse(raw["governance"])
	if err != nil {
		errs = append(errs, fmt.Sprintf("governance: %v", err))
	}
	cfg.Governance = gov

	if len(cfg.MIBs) == 0 && len(cfg.Custom) == 0 {
		errs = append(errs, "at least one entry under 'mibs' or 'custom_mappings' is required")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("snmp_poll config: %s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

func resolveVersion(s string) (gosnmp.SnmpVersion, error) {
	v, err := snmpcore.ParseVersion(s)
	if err != nil {
		return 0, fmt.Errorf("%v (use \"v2c\" or \"v3\")", err)
	}
	if v == gosnmp.Version1 {
		return 0, fmt.Errorf("SNMPv1 is not supported (table walks need GETBULK; use v2c)")
	}
	return v, nil
}

// parseV3 validates the optional "v3:" block. Required when the version
// is v3; rejected otherwise (a silently ignored credentials block would
// hide a misconfiguration). Unknown protocol names are errors, not a
// silent downgrade to noAuth/noPriv.
func parseV3(raw interface{}, wantV3 bool) (*v3Config, error) {
	if raw == nil {
		if wantV3 {
			return nil, fmt.Errorf("version v3 requires a 'v3' block with at least 'username'")
		}
		return nil, nil
	}
	if !wantV3 {
		return nil, fmt.Errorf("'v3' block is set but version is not v3")
	}
	m, ok := toStringMap(raw)
	if !ok {
		return nil, fmt.Errorf("'v3' must be a mapping")
	}

	v3 := &v3Config{
		Username:     strings.TrimSpace(strOf(m["username"])),
		AuthProtocol: strings.ToUpper(strings.TrimSpace(strOf(m["auth_protocol"]))),
		AuthPassword: strOf(m["auth_passphrase"]),
		PrivProtocol: strings.ToUpper(strings.TrimSpace(strOf(m["priv_protocol"]))),
		PrivPassword: strOf(m["priv_passphrase"]),
	}
	if v3.Username == "" {
		return nil, fmt.Errorf("'v3.username' is required")
	}
	if !snmpcore.KnownAuthProtocol(v3.AuthProtocol) {
		return nil, fmt.Errorf("'v3.auth_protocol' %q is not supported (MD5, SHA, SHA224, SHA256, SHA384, SHA512)", v3.AuthProtocol)
	}
	if !snmpcore.KnownPrivProtocol(v3.PrivProtocol) {
		return nil, fmt.Errorf("'v3.priv_protocol' %q is not supported (DES, AES, AES192, AES256)", v3.PrivProtocol)
	}
	if v3.AuthProtocol != "" && v3.AuthPassword == "" {
		return nil, fmt.Errorf("'v3.auth_passphrase' is required with auth_protocol")
	}
	if v3.PrivProtocol != "" && v3.AuthProtocol == "" {
		return nil, fmt.Errorf("'v3.priv_protocol' requires an auth_protocol (USM authPriv builds on auth)")
	}
	if v3.PrivProtocol != "" && v3.PrivPassword == "" {
		return nil, fmt.Errorf("'v3.priv_passphrase' is required with priv_protocol")
	}
	return v3, nil
}

func parseMIBs(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("'mibs' must be a list")
	}
	known := builtinMIBNames()
	out := make([]string, 0, len(list))
	for _, item := range list {
		name, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("'mibs' entries must be strings")
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if !known[name] {
			// Reject unknown selectors loudly so a typo or a not-yet-built
			// module name is not silently ignored.
			return nil, fmt.Errorf("unknown built-in MIB %q (supported: mib-2, if-mib)", name)
		}
		out = append(out, name)
	}
	return out, nil
}

func parseCustomMappings(v interface{}, haveMIBs bool) ([]customMapping, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("'custom_mappings' must be a list")
	}
	out := make([]customMapping, 0, len(list))
	for i, item := range list {
		m, ok := toStringMap(item)
		if !ok {
			return nil, fmt.Errorf("'custom_mappings'[%d] must be a mapping", i)
		}
		oid := snmpcore.TrimLeadingDot(strOf(m["oid"]))
		if oid == "" {
			return nil, fmt.Errorf("'custom_mappings'[%d] requires 'oid'", i)
		}
		metric := strings.TrimSpace(strOf(m["metric"]))
		if metric == "" && !haveMIBs {
			return nil, fmt.Errorf("'custom_mappings'[%d] requires 'metric' (or configure mib_paths so the name can be resolved from the MIBs)", i)
		}
		kind := kindGauge
		if strings.ToLower(strings.TrimSpace(strOf(m["type"]))) == "counter" {
			kind = kindCounter
		}
		out = append(out, customMapping{
			OID:        oid,
			Metric:     metric,
			Kind:       kind,
			IndexLabel: strings.TrimSpace(strOf(m["index_label"])),
		})
	}
	return out, nil
}

// parseDiscovery validates the optional "discovery:" block. Returns nil when
// absent (single-target mode). Requires seeds (valid IPs), a v2c profile with a
// community, and at least one allowed CIDR so the crawl cannot wander off the
// managed network; max_devices / max_hops default to 200 / 4.
func parseDiscovery(v interface{}) (*discoveryConfig, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := toStringMap(v)
	if !ok {
		return nil, fmt.Errorf("'discovery' must be a mapping")
	}

	d := &discoveryConfig{MaxDevices: defaultMaxDevices, MaxHops: defaultMaxHops}

	seeds, err := parseIPList(m["seeds"])
	if err != nil {
		return nil, fmt.Errorf("discovery.seeds: %w", err)
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("discovery.seeds is required (at least one entry device IP)")
	}
	d.Seeds = seeds

	prof, ok := toStringMap(m["profile"])
	if !ok {
		return nil, fmt.Errorf("discovery.profile is required (a mapping with version + community)")
	}
	if vs := strings.TrimSpace(strOf(prof["version"])); vs != "" {
		v, err := resolveVersion(vs)
		if err != nil {
			return nil, fmt.Errorf("discovery.profile.version: %w", err)
		}
		if v != gosnmp.Version2c {
			return nil, fmt.Errorf("discovery.profile.version: the crawl profile is v2c-only for now (per-device v3 polling is supported on 'version'/'v3')")
		}
	}
	d.Profile.Version = "v2c"
	d.Profile.Community = strings.TrimSpace(strOf(prof["community"]))
	if d.Profile.Community == "" {
		return nil, fmt.Errorf("discovery.profile.community is required (v2c)")
	}

	cidrs, err := parseCIDRList(m["allowed_cidrs"])
	if err != nil {
		return nil, fmt.Errorf("discovery.allowed_cidrs: %w", err)
	}
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("discovery.allowed_cidrs is required (bounds the crawl to the managed network)")
	}
	d.AllowedCIDRs = cidrs

	if n, ok := types.IntParam(m, "max_devices"); ok {
		if n <= 0 {
			return nil, fmt.Errorf("discovery.max_devices must be positive")
		}
		d.MaxDevices = n
	}
	if n, ok := types.IntParam(m, "max_hops"); ok {
		if n < 1 {
			return nil, fmt.Errorf("discovery.max_hops must be at least 1")
		}
		d.MaxHops = n
	}
	if iv, ok, err := durationParam(m, "interval"); err != nil {
		return nil, fmt.Errorf("discovery.interval: %w", err)
	} else if ok {
		d.Interval = iv
	}

	rules, err := governance.ParseRules(m["governance_rules"])
	if err != nil {
		return nil, fmt.Errorf("discovery.%w", err)
	}
	d.GovernanceRules = rules

	return d, nil
}

// parseIPList parses a YAML list of IP-address strings, rejecting malformed
// entries.
func parseIPList(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be a list")
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s := strings.TrimSpace(strOf(item))
		if net.ParseIP(s) == nil {
			return nil, fmt.Errorf("%q is not a valid IP", s)
		}
		out = append(out, s)
	}
	return out, nil
}

// parseCIDRList parses a YAML list of CIDR strings into networks.
func parseCIDRList(v interface{}) ([]*net.IPNet, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("must be a list")
	}
	out := make([]*net.IPNet, 0, len(list))
	for _, item := range list {
		s := strings.TrimSpace(strOf(item))
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid CIDR", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// durationParam reads a duration that may be written as a duration string
// ("5s") or a bare number of seconds (5). Returns ok=false when absent.
func durationParam(raw map[string]interface{}, key string) (time.Duration, bool, error) {
	v, present := raw[key]
	if !present {
		return 0, false, nil
	}
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return 0, false, nil
		}
		d, err := time.ParseDuration(strings.TrimSpace(s))
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		if d <= 0 {
			return 0, false, fmt.Errorf("must be positive")
		}
		return d, true, nil
	}
	if n, ok := types.IntParam(raw, key); ok {
		if n <= 0 {
			return 0, false, fmt.Errorf("must be a positive number of seconds")
		}
		return time.Duration(n) * time.Second, true, nil
	}
	return 0, false, fmt.Errorf("expected a duration string or a number of seconds")
}

// toStringMap coerces a YAML/JSON-decoded nested mapping to
// map[string]interface{}; yaml.v2 yields map[interface{}]interface{},
// JSON yields map[string]interface{} — accept both.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprintf("%v", k)] = val
		}
		return out, true
	}
	return nil, false
}

func strOf(v interface{}) string {
	s, _ := v.(string)
	return s
}

// stringSliceOf coerces a YAML-decoded value into []string (the loader
// hands lists over as []interface{}). Mirrors snmptrap's helper.
func stringSliceOf(raw interface{}) []string {
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
