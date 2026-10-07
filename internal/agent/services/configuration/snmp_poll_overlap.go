package configuration

import (
	"net"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/logger"
)

const (
	snmpPollType        = "snmp_poll"
	snmpPollDefaultPort = 161
	snmpPollDefaultComm = "public"
)

// SNMPTargetOverlap names the enabled snmp_poll probes that poll the same
// device with the same credential. Each of them sends its own requests, so
// the device is read once per probe and its metrics arrive duplicated.
type SNMPTargetOverlap struct {
	// Target is the normalized host:port the probes share.
	Target string
	// Probes are the probe names, in configuration order.
	Probes []string
}

// SNMPPollTargetOverlaps lists every device polled by more than one enabled
// snmp_poll probe under the same credential: the same normalized host and
// port with the same community, or the same SNMPv3 user. The same device
// under two different credentials is a legitimate setup (a read-only and a
// read-write community, two views) and is not reported.
func SNMPPollTargetOverlaps(list []ProbeConfig) []SNMPTargetOverlap {
	owners := map[string][]string{}
	display := map[string]string{}
	var order []string
	for _, p := range list {
		if p.Type != snmpPollType || !p.IsEnabled() {
			continue
		}
		hostPort, credential, ok := snmpPollIdentity(p.Params)
		if !ok {
			continue
		}
		key := hostPort + "\x00" + credential
		if len(owners[key]) == 0 {
			order = append(order, key)
			display[key] = hostPort
		}
		owners[key] = append(owners[key], p.Name)
	}
	var out []SNMPTargetOverlap
	for _, key := range order {
		if len(owners[key]) > 1 {
			out = append(out, SNMPTargetOverlap{Target: display[key], Probes: owners[key]})
		}
	}
	return out
}

// snmpPollIdentity reduces a probe's parameters to what the device sees:
// where the request goes and who it claims to be. ok is false when the
// probe has no usable target, which the probe itself rejects at start.
func snmpPollIdentity(params map[string]interface{}) (hostPort, credential string, ok bool) {
	target, _ := params["target"].(string)
	host := strings.ToLower(strings.TrimSuffix(strings.Trim(strings.TrimSpace(target), "[]"), "."))
	if host == "" {
		return "", "", false
	}
	port := snmpPollDefaultPort
	switch v := params["port"].(type) {
	case int:
		port = v
	case int64:
		port = int(v)
	case uint64:
		port = int(v)
	case float64:
		port = int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			port = n
		}
	}
	hostPort = net.JoinHostPort(host, strconv.Itoa(port))

	if v3, present := params["v3"]; present && v3 != nil {
		if m, isMap := v3.(map[string]interface{}); isMap {
			user, _ := m["username"].(string)
			return hostPort, "v3:" + strings.TrimSpace(user), true
		}
	}
	community := snmpPollDefaultComm
	if c, isString := params["community"].(string); isString && c != "" {
		community = c
	}
	return hostPort, "c:" + community, true
}

// warnSNMPPollOverlaps logs one Warn per shared device. Credentials are
// never named: the line carries probe names and the target only.
func warnSNMPPollOverlaps(log *logger.ModuleLogger, list []ProbeConfig) {
	if log == nil {
		return
	}
	for _, o := range SNMPPollTargetOverlaps(list) {
		log.Warn().
			Str("target", o.Target).
			Strs("probes", o.Probes).
			Msg("snmp_poll probes poll the same device with the same credential; it is read once per probe and its metrics are duplicated")
	}
}
