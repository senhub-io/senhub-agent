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
	"time"

	"github.com/gosnmp/gosnmp"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/snmpmib"
)

// SNMPTrapProbe is the trap receiver. Event-driven: Collect returns nil
// and the gosnmp TrapListener pushes records onto the agent log channel
// as traps arrive.
type SNMPTrapProbe struct {
	*types.BaseProbe
	config       receiverConfig
	moduleLogger *logger.ModuleLogger

	mu       sync.Mutex
	listener *gosnmp.TrapListener
	mibs     *snmpmib.Resolver
	quitOnce sync.Once
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
func (p *SNMPTrapProbe) Collect() ([]data_store.DataPoint, error) { return nil, nil }

// OnStart opens the trap listener and starts serving. A bind failure
// (e.g. port 162 without privileges, or address already in use) is
// returned so the framework marks the probe unhealthy.
func (p *SNMPTrapProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Str("bind_address", p.config.BindAddress).
		Str("version", p.config.Version).
		Strs("mib_paths", p.config.MibPaths).
		Msg("Starting snmp_trap probe")

	// Load operator-supplied local MIBs (never fetched) so trap/varbind
	// OIDs resolve to names. Safe with no paths (disabled resolver).
	p.mibs = snmpmib.Load(p.config.MibPaths, p.moduleLogger)

	tl := gosnmp.NewTrapListener()
	tl.OnNewTrap = p.handleTrap
	params, err := p.buildParams()
	if err != nil {
		return err
	}
	tl.Params = params

	p.mu.Lock()
	p.listener = tl
	p.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if listenErr := tl.Listen(p.config.BindAddress); listenErr != nil {
			errCh <- listenErr
		}
	}()

	// Surface a bind error synchronously: Listen returns it fast on
	// failure, and signals Listening() once the socket is open.
	select {
	case <-tl.Listening():
		p.moduleLogger.Info().Str("bind_address", p.config.BindAddress).Msg("SNMP trap receiver listening")
	case err := <-errCh:
		return fmt.Errorf("snmp_trap: listen on %s: %w", p.config.BindAddress, err)
	case <-time.After(2 * time.Second):
		p.moduleLogger.Warn().Str("bind_address", p.config.BindAddress).
			Msg("snmp_trap listener readiness not confirmed within 2s; assuming up")
	}

	go func() {
		<-quitChannel
		p.quitOnce.Do(func() {
			p.moduleLogger.Info().Msg("Quit signal received; stopping snmp_trap receiver")
			p.closeListener()
		})
	}()

	return nil
}

// OnShutdown closes the listener.
func (p *SNMPTrapProbe) OnShutdown(_ context.Context) error {
	p.closeListener()
	return nil
}

func (p *SNMPTrapProbe) closeListener() {
	p.mu.Lock()
	tl := p.listener
	p.listener = nil
	p.mu.Unlock()
	if tl != nil {
		tl.Close()
	}
}

// handleTrap is the gosnmp callback: decode the packet into a LogRecord
// and publish it. Must not retain references into s/u (gosnmp reuses
// them) — packetToLogRecord copies out everything it needs.
func (p *SNMPTrapProbe) handleTrap(s *gosnmp.SnmpPacket, u *net.UDPAddr) {
	sourceIP := ""
	if u != nil {
		sourceIP = u.IP.String()
	}
	rec := packetToLogRecord(s, sourceIP, p.GetName(), p.mibs)
	agentstate.PublishLog(rec)
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
		auth := authProtocol(u.AuthProtocol)
		priv := privProtocol(u.PrivProtocol)
		params.MsgFlags = msgFlags(auth, priv)
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

func authProtocol(name string) gosnmp.SnmpV3AuthProtocol {
	switch name {
	case "MD5":
		return gosnmp.MD5
	case "SHA":
		return gosnmp.SHA
	case "SHA224":
		return gosnmp.SHA224
	case "SHA256":
		return gosnmp.SHA256
	case "SHA384":
		return gosnmp.SHA384
	case "SHA512":
		return gosnmp.SHA512
	default:
		return gosnmp.NoAuth
	}
}

func privProtocol(name string) gosnmp.SnmpV3PrivProtocol {
	switch name {
	case "DES":
		return gosnmp.DES
	case "AES":
		return gosnmp.AES
	case "AES192":
		return gosnmp.AES192
	case "AES256":
		return gosnmp.AES256
	default:
		return gosnmp.NoPriv
	}
}

func msgFlags(auth gosnmp.SnmpV3AuthProtocol, priv gosnmp.SnmpV3PrivProtocol) gosnmp.SnmpV3MsgFlags {
	switch {
	case auth != gosnmp.NoAuth && priv != gosnmp.NoPriv:
		return gosnmp.AuthPriv
	case auth != gosnmp.NoAuth:
		return gosnmp.AuthNoPriv
	default:
		return gosnmp.NoAuthNoPriv
	}
}

// gosnmpLog routes gosnmp's internal logging (unmarshal errors, dropped
// packets, USM engine-id mismatches) to the probe's module logger at
// debug level, so trap-decode failures are diagnosable without noise at
// the default log level.
type gosnmpLog struct{ l *logger.ModuleLogger }

func (g gosnmpLog) Print(v ...interface{})                 { g.l.Debug().Msg(fmt.Sprint(v...)) }
func (g gosnmpLog) Printf(format string, v ...interface{}) { g.l.Debug().Msgf(format, v...) }
