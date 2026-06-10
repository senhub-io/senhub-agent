package snmppoll

import (
	"fmt"
	"net"
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

// config is the resolved, validated snmp_poll probe configuration.
type config struct {
	Target    string
	Port      uint16
	Community string
	Timeout   time.Duration
	Retries   int
	Interval  time.Duration
	// TopologyInterval is the (slow) cadence for the entity-rail topology
	// sweep, independent of the metric Interval. Zero → defaultTopologyInterval.
	TopologyInterval time.Duration

	// MIBs is the ordered list of built-in MIB selectors to poll
	// (e.g. "mib-2", "if-mib").
	MIBs []string
	// Custom holds operator OID mappings beyond the built-in modules.
	Custom []customMapping

	// Discovery, when set, enables the SNMP crawl: from the seeds the probe
	// expands the poll set across the LLDP neighbour graph, bounded. nil when
	// the "discovery:" block is absent (single-target mode). The multi-target
	// poll lifecycle that consumes it is a later lot.
	Discovery *discoveryConfig
}

// discoveryConfig is the validated "discovery:" block.
type discoveryConfig struct {
	Seeds        []string         // entry device IPs
	Profile      discoveryProfile // single credential profile for discovered devices
	MaxDevices   int              // hard cap on discovered devices
	MaxHops      int              // BFS depth bound
	AllowedCIDRs []*net.IPNet     // neighbours are crawled only within these
	Interval     time.Duration    // crawl cadence; 0 → topology interval
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

	if vs, ok := raw["version"].(string); ok && strings.TrimSpace(vs) != "" {
		if err := checkVersion(vs); err != nil {
			errs = append(errs, err.Error())
		}
	}

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

	custom, err := parseCustomMappings(raw["custom_mappings"])
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.Custom = custom

	disc, err := parseDiscovery(raw["discovery"])
	if err != nil {
		errs = append(errs, err.Error())
	}
	cfg.Discovery = disc

	if len(cfg.MIBs) == 0 && len(cfg.Custom) == 0 {
		errs = append(errs, "at least one entry under 'mibs' or 'custom_mappings' is required")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("snmp_poll config: %s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

func checkVersion(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "2", "2c", "v2c":
		return nil
	case "1", "v1":
		return fmt.Errorf("SNMPv1 is not supported (table walks need GETBULK; use v2c)")
	case "3", "v3":
		return fmt.Errorf("SNMPv3 is not supported yet (planned for a later lot)")
	default:
		return fmt.Errorf("unsupported version %q (use \"v2c\")", s)
	}
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

func parseCustomMappings(v interface{}) ([]customMapping, error) {
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
		oid := trimLeadingDot(strOf(m["oid"]))
		if oid == "" {
			return nil, fmt.Errorf("'custom_mappings'[%d] requires 'oid'", i)
		}
		metric := strings.TrimSpace(strOf(m["metric"]))
		if metric == "" {
			return nil, fmt.Errorf("'custom_mappings'[%d] requires 'metric'", i)
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
		if err := checkVersion(vs); err != nil {
			return nil, fmt.Errorf("discovery.profile.version: %w", err)
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
