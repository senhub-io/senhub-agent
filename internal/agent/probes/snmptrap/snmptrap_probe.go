// Package snmptrap implements an SNMP trap receiver probe.
//
// The probe listens on a UDP socket for SNMP v2c / v3 traps (and informs)
// emitted by network equipment — switches, UPS, industrial gear — and
// relays each one as an OTel log record (publishes to the agent log
// channel, like linux_logs / syslog). It is the push counterpart of the
// snmp_poll probe and reuses the same gosnmp dependency. Free tier
// (universal collection).
//
// Trap OID → name resolution: the six generic SNMPv2-MIB traps resolve
// from a compiled-in table (traps.go); vendor OIDs resolve from
// operator-supplied LOCAL MIB files loaded at startup via the shared
// snmpmib package (config `mib_paths`). The agent NEVER fetches MIBs over
// the network — only local files the operator provides. Unresolved OIDs
// surface by their numeric form.
//
// Port 162 is privileged (<1024): binding the default 0.0.0.0:162 needs
// root or CAP_NET_BIND_SERVICE (see issue #223). Use a high port for
// unprivileged setups.
//
// SNMPv3 note: gosnmp's trap listener carries a single USM identity and
// upstream flags v3 trap handling as not fully reliable; v3 is wired
// best-effort using the first configured user. v2c is the solid path.
package snmptrap

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/snmpcore"
	"senhub-agent.go/internal/agent/services/snmpmib"
	"senhub-agent.go/internal/agent/utils/netbind"
)

// SNMPTrapProbe is the trap receiver. Event-driven: Collect returns nil
// and a dedicated UDP read loop pushes records onto the agent log channel
// as traps arrive.
//
// It runs its own net.ListenUDP loop rather than gosnmp's TrapListener:
// the latter does not receive datagrams on Windows (a plain net.ListenUDP
// socket does), so we own the socket and only borrow gosnmp's exported
// UnmarshalTrap to decode each packet. See issue #226 / the Windows
// runtime-validation finding.
type SNMPTrapProbe struct {
	*types.BaseProbe
	config       receiverConfig
	moduleLogger *logger.ModuleLogger

	mu        sync.Mutex
	conn      *net.UDPConn
	params    *gosnmp.GoSNMP
	mibs      *snmpmib.Resolver
	quitOnce  sync.Once
	firstTrap sync.Once

	// Receiver self-metrics (#263): datagrams rejected for a community
	// mismatch and decode/handle panics recovered per-datagram.
	rejectedCommunity atomic.Uint64
	decodePanics      atomic.Uint64

	// decode is the datagram decoder, defaulting to params.UnmarshalTrap.
	// A test seam: hostile-input tests inject panicking/failing decoders.
	decode func(data []byte, useResponseSecurityParameters bool) (*gosnmp.SnmpPacket, error)
}

// NewSNMPTrapProbe constructs the probe. Config errors (bad version,
// missing v3 user) surface here; bind errors surface at OnStart.
func NewSNMPTrapProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.snmp_trap")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	moduleLogger.Debug().
		Str("bind_address", cfg.BindAddress).
		Str("version", cfg.Version).
		Msg("Creating new snmp_trap probe")

	p := &SNMPTrapProbe{
		BaseProbe:    &types.BaseProbe{},
		config:       cfg,
		moduleLogger: moduleLogger,
	}
	p.SetProbeType(ProbeType)
	return p, nil
}

// GetTargetStrategies returns an empty list — this probe publishes to the
// agentstate log channel directly, like linux_logs.
func (p *SNMPTrapProbe) GetTargetStrategies() []string { return []string{} }

// ShouldStart always returns true; binding happens in OnStart.
func (p *SNMPTrapProbe) ShouldStart() bool { return true }

// GetInterval is irrelevant for an event-driven probe but the poller
// requires a value.
func (p *SNMPTrapProbe) GetInterval() time.Duration { return 5 * time.Minute }

// Collect is a no-op: traps arrive via the listener and are published to
// the log channel as they come.
// Collect emits the receiver's self-metrics: cumulative counts of
// community-rejected datagrams and recovered decode panics (#263). The
// trap payloads themselves ride the log rail, not Collect.
func (p *SNMPTrapProbe) Collect() ([]data_store.DataPoint, error) {
	now := time.Now()
	points := []data_store.DataPoint{
		{Name: "senhub.snmp_trap.rejected_community", Value: float64(p.rejectedCommunity.Load()), Timestamp: now},
		{Name: "senhub.snmp_trap.decode_panics", Value: float64(p.decodePanics.Load()), Timestamp: now},
	}
	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

// OnStart opens the UDP socket and starts the read loop. A bind failure
// (e.g. port 162 without privileges, or address already in use) is
// returned synchronously so the framework marks the probe unhealthy —
// net.ListenUDP opens the socket before returning, so there is no
// readiness race to guard against.
func (p *SNMPTrapProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Str("bind_address", p.config.BindAddress).
		Str("version", p.config.Version).
		Strs("mib_paths", p.config.MibPaths).
		Msg("Starting snmp_trap probe")

	if netbind.IsWildcard(p.config.BindAddress) {
		p.moduleLogger.Warn().
			Str("bind_address", p.config.BindAddress).
			Msg("SNMP trap receiver bound to ALL interfaces — restrict `bind_address` or firewall the port")
	}

	// Load operator-supplied local MIBs (never fetched) so trap/varbind
	// OIDs resolve to names. Safe with no paths (disabled resolver).
	p.mibs = snmpmib.Load(p.config.MibPaths, p.moduleLogger)

	params, err := p.buildParams()
	if err != nil {
		return err
	}
	p.params = params

	udpAddr, err := net.ResolveUDPAddr("udp", p.config.BindAddress)
	if err != nil {
		return fmt.Errorf("snmp_trap: resolve %s: %w", p.config.BindAddress, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("snmp_trap: listen on %s: %w", p.config.BindAddress, err)
	}

	p.mu.Lock()
	p.conn = conn
	p.mu.Unlock()

	p.moduleLogger.Info().Str("bind_address", p.config.BindAddress).Msg("SNMP trap receiver listening")

	go p.serve(conn)

	go func() {
		<-quitChannel
		p.quitOnce.Do(func() {
			p.moduleLogger.Info().Msg("Quit signal received; stopping snmp_trap receiver")
			p.closeListener()
		})
	}()

	return nil
}

// serve is the UDP read loop. It blocks on ReadFromUDP, decodes each
// datagram with gosnmp's UnmarshalTrap, and hands valid packets to
// handleTrap. A read error after the socket has been closed (nil conn)
// ends the loop cleanly; any other read or decode error is logged at
// debug and the loop continues — a malformed packet must not take the
// receiver down.
func (p *SNMPTrapProbe) serve(conn *net.UDPConn) {
	buf := make([]byte, 4096)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			p.mu.Lock()
			closed := p.conn == nil
			p.mu.Unlock()
			if closed {
				return
			}
			p.moduleLogger.Debug().Err(err).Msg("snmp_trap: UDP read error")
			continue
		}

		// Copy out: UnmarshalTrap may retain slices, and buf is reused on
		// the next read.
		msg := make([]byte, n)
		copy(msg, buf[:n])

		p.processDatagram(msg, remote)
	}
}

// processDatagram decodes and handles one raw datagram. Hardened per
// #263: a decode/handle panic on attacker-controlled bytes is recovered
// (counted, never fatal), and the v2c/v1 community is compared against
// the configured one before the packet is accepted — previously the
// community was documented as authenticating v2c but never checked, so
// any reachable UDP sender could inject forged log records (and have
// informs acked back, a reflection primitive).
func (p *SNMPTrapProbe) processDatagram(msg []byte, remote *net.UDPAddr) {
	defer func() {
		if r := recover(); r != nil {
			p.decodePanics.Add(1)
			p.moduleLogger.Error().
				Interface("panic", r).
				Str("source_ip", remoteIP(remote)).
				Msg("snmp_trap: recovered panic while decoding/handling datagram")
		}
	}()

	decode := p.decode
	if decode == nil {
		decode = p.params.UnmarshalTrap
	}
	trap, err := decode(msg, false)
	if err != nil {
		p.moduleLogger.Debug().Err(err).
			Str("source_ip", remoteIP(remote)).
			Msg("snmp_trap: failed to decode datagram")
		return
	}

	// Authenticate v1/v2c by community. v3 is authenticated by gosnmp's
	// USM layer inside UnmarshalTrap. Rejected datagrams are counted and
	// never handled nor acked.
	if trap.Version != gosnmp.Version3 && p.config.Community != "" && trap.Community != p.config.Community {
		p.rejectedCommunity.Add(1)
		p.moduleLogger.Debug().
			Str("source_ip", remoteIP(remote)).
			Msg("snmp_trap: rejected datagram with mismatched community")
		return
	}

	p.handleTrap(trap, remote)

	// An InformRequest is a confirmed notification: the sender keeps
	// retransmitting (producing duplicate records) until it receives
	// a GetResponse. Acknowledge v2c informs from the raw datagram.
	if trap.PDUType == gosnmp.InformRequest {
		p.ackInform(trap, msg, remote)
	}
}

func remoteIP(u *net.UDPAddr) string {
	if u == nil {
		return ""
	}
	return u.IP.String()
}

// ackInform replies to a v2c InformRequest with its GetResponse so the
// sender stops retransmitting. v3 informs are not acked (the scoped PDU
// may be encrypted; v3 is best-effort) — they are still logged, but a
// v3 inform sender may retransmit.
func (p *SNMPTrapProbe) ackInform(trap *gosnmp.SnmpPacket, raw []byte, remote *net.UDPAddr) {
	if trap.Version != gosnmp.Version2c {
		p.moduleLogger.Debug().Str("source_ip", remote.IP.String()).
			Msg("snmp_trap: inform acknowledgement only supported for v2c; sender may retransmit")
		return
	}
	ack, ok := buildInformAck(raw)
	if !ok {
		p.moduleLogger.Debug().Str("source_ip", remote.IP.String()).
			Msg("snmp_trap: could not build inform ack; sender may retransmit")
		return
	}

	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return
	}
	if _, err := conn.WriteToUDP(ack, remote); err != nil {
		p.moduleLogger.Debug().Err(err).Str("source_ip", remote.IP.String()).
			Msg("snmp_trap: failed to send inform ack")
	}
}

// OnShutdown closes the listener.
func (p *SNMPTrapProbe) OnShutdown(_ context.Context) error {
	p.closeListener()
	return nil
}

func (p *SNMPTrapProbe) closeListener() {
	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// handleTrap turns a decoded packet into a LogRecord and publishes it.
// Called serially from serve. Must not retain references into s/u —
// packetToLogRecord copies out everything it needs.
func (p *SNMPTrapProbe) handleTrap(s *gosnmp.SnmpPacket, u *net.UDPAddr) {
	sourceIP := ""
	if u != nil {
		sourceIP = u.IP.String()
	}
	rec := packetToLogRecord(s, sourceIP, p.GetName(), p.mibs)
	agentstate.PublishLog(rec)
	// The trap itself is the output (an OTel log record); per-trap logging
	// stays at debug to avoid duplicating a high-volume stream. Surface the
	// FIRST trap at info, though, so an operator gets a one-line "the
	// receiver is actually getting traps" confirmation without enabling
	// debug.
	p.firstTrap.Do(func() {
		p.moduleLogger.Info().
			Str("source_ip", sourceIP).
			Str("trap_oid", rec.Attributes["trap_oid"]).
			Str("trap_name", rec.Attributes["trap_name"]).
			Msg("First SNMP trap received (receiver is working)")
	})
	p.moduleLogger.Debug().
		Str("source_ip", sourceIP).
		Str("trap_oid", rec.Attributes["trap_oid"]).
		Str("trap_name", rec.Attributes["trap_name"]).
		Msg("Received SNMP trap")
}

// buildParams assembles the gosnmp parameters for the listener from the
// configured version/community/USM credentials.
func (p *SNMPTrapProbe) buildParams() (*gosnmp.GoSNMP, error) {
	params := &gosnmp.GoSNMP{Logger: gosnmp.NewLogger(gosnmpLog{p.moduleLogger})}

	switch p.config.Version {
	case "v2c":
		params.Version = gosnmp.Version2c
		params.Community = p.config.Community
	case "v3":
		params.Version = gosnmp.Version3
		params.SecurityModel = gosnmp.UserSecurityModel
		u := p.config.V3Users[0] // validated non-empty in parseConfig
		auth := snmpcore.AuthProtocol(u.AuthProtocol)
		priv := snmpcore.PrivProtocol(u.PrivProtocol)
		params.MsgFlags = snmpcore.MsgFlags(auth, priv)
		params.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 u.Username,
			AuthenticationProtocol:   auth,
			AuthenticationPassphrase: u.AuthPassword,
			PrivacyProtocol:          priv,
			PrivacyPassphrase:        u.PrivPassword,
		}
	default:
		return nil, fmt.Errorf("snmp_trap: unsupported version %q", p.config.Version)
	}
	return params, nil
}

// gosnmpLog routes gosnmp's internal logging (unmarshal errors, dropped
// packets, USM engine-id mismatches) to the probe's module logger at
// debug level, so trap-decode failures are diagnosable without noise at
// the default log level.
type gosnmpLog struct{ l *logger.ModuleLogger }

func (g gosnmpLog) Print(v ...interface{})                 { g.l.Debug().Msg(fmt.Sprint(v...)) }
func (g gosnmpLog) Printf(format string, v ...interface{}) { g.l.Debug().Msgf(format, v...) }
