package snmptrap

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"
	"senhub-agent.go/internal/agent/services/logger"
)

// TrapHandler is a function that handles received traps
type TrapHandler func(packet *gosnmp.SnmpPacket, addr string)

// TrapListener listens for SNMP traps on UDP port
type TrapListener struct {
	config      *Config
	handler     TrapHandler
	listener    *gosnmp.TrapListener
	running     int32
	shutdownCh  chan struct{}
	wg          sync.WaitGroup
	logger      *logger.Logger
	
	// Rate limiting
	rateLimiter *RateLimiter
	
	// Statistics
	stats struct {
		trapsReceived int64
		trapsFiltered int64
		trapsRateLimited int64
		lastReceiveTime time.Time
	}
}

// NewTrapListener creates a new SNMP trap listener
func NewTrapListener(config *Config, handler TrapHandler, logger *logger.Logger) *TrapListener {
	tl := &TrapListener{
		config:      config,
		handler:     handler,
		shutdownCh:  make(chan struct{}),
		logger:      logger,
	}
	
	// Initialize rate limiter
	tl.rateLimiter = NewRateLimiter(
		config.Filters.RateLimit.MaxTrapsPerMinute,
		config.Filters.RateLimit.PerSourceLimit,
		logger,
	)
	
	return tl
}

// Start starts the trap listener
func (tl *TrapListener) Start() error {
	if atomic.LoadInt32(&tl.running) == 1 {
		return fmt.Errorf("trap listener already running")
	}
	
	tl.logger.Info().
		Str("address", tl.config.ListenAddress).
		Msg("Starting SNMP trap listener")
	
	// Create gosnmp trap listener
	tl.listener = gosnmp.NewTrapListener()
	tl.listener.OnNewTrap = tl.handleTrap
	
	// Configure listener parameters
	tl.listener.Params = gosnmp.Default
	tl.listener.Params.Logger = gosnmp.NewLogger(&snmpLogger{logger: tl.logger})
	
	// Mark as running before starting goroutine
	atomic.StoreInt32(&tl.running, 1)
	
	// Start listening in background
	errChan := make(chan error, 1)
	tl.wg.Add(1)
	go func() {
		defer tl.wg.Done()
		
		tl.logger.Debug().
			Str("address", tl.config.ListenAddress).
			Msg("Starting gosnmp Listen()")
		
		err := tl.listener.Listen(tl.config.ListenAddress)
		if err != nil {
			tl.logger.Error().
				Err(err).
				Str("address", tl.config.ListenAddress).
				Msg("Trap listener error")
			atomic.StoreInt32(&tl.running, 0)
			select {
			case errChan <- err:
			default:
			}
		} else {
			tl.logger.Debug().Msg("Trap listener stopped cleanly")
		}
	}()
	
	// Wait a moment for listener to start
	time.Sleep(200 * time.Millisecond)
	
	// Check for immediate startup error
	select {
	case err := <-errChan:
		tl.logger.Error().
			Err(err).
			Str("address", tl.config.ListenAddress).
			Msg("Failed to start listener")
		atomic.StoreInt32(&tl.running, 0)
		return err
	default:
		// No immediate error, listener started successfully
	}
	
	// Start maintenance goroutine
	tl.wg.Add(1)
	go tl.maintenanceLoop()
	
	tl.logger.Info().
		Str("address", tl.config.ListenAddress).
		Msg("SNMP trap listener started successfully")
	
	return nil
}

// Stop stops the trap listener
func (tl *TrapListener) Stop() error {
	if atomic.LoadInt32(&tl.running) == 0 {
		return nil
	}
	
	tl.logger.Info().Msg("Stopping SNMP trap listener")
	
	atomic.StoreInt32(&tl.running, 0)
	
	// Signal shutdown
	close(tl.shutdownCh)
	
	// Close the listener
	if tl.listener != nil {
		tl.listener.Close()
	}
	
	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		tl.wg.Wait()
		close(done)
	}()
	
	// Wait with timeout
	select {
	case <-done:
		tl.logger.Info().Msg("Trap listener stopped successfully")
	case <-time.After(5 * time.Second):
		tl.logger.Warn().Msg("Trap listener stop timeout")
	}
	
	return nil
}

// handleTrap processes incoming SNMP traps
func (tl *TrapListener) handleTrap(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
	atomic.AddInt64(&tl.stats.trapsReceived, 1)
	tl.stats.lastReceiveTime = time.Now()
	
	sourceAddr := addr.String()
	sourceIP := extractIP(sourceAddr)
	
	tl.logger.Debug().
		Str("source", sourceAddr).
		Str("version", getVersionString(packet.Version)).
		Str("community", string(packet.Community)).
		Int("pdu_type", int(packet.PDUType)).
		Msg("Received SNMP packet")
	
	// Check if it's a trap PDU
	if packet.PDUType != gosnmp.Trap && packet.PDUType != gosnmp.SNMPv2Trap {
		tl.logger.Warn().
			Str("source", sourceAddr).
			Int("pdu_type", int(packet.PDUType)).
			Msg("Received non-trap SNMP packet, ignoring")
		return
	}
	
	// Apply source filtering
	if !tl.isSourceAllowed(sourceIP) {
		atomic.AddInt64(&tl.stats.trapsFiltered, 1)
		tl.logger.Debug().
			Str("source", sourceIP).
			Msg("Trap filtered by source rules")
		return
	}
	
	// Apply rate limiting
	if !tl.rateLimiter.Allow(sourceIP) {
		atomic.AddInt64(&tl.stats.trapsRateLimited, 1)
		tl.logger.Warn().
			Str("source", sourceIP).
			Msg("Trap rate limited")
		return
	}
	
	// Check community string (SNMPv1/v2c only)
	if packet.Version != gosnmp.Version3 {
		if !tl.isCommunityAllowed(string(packet.Community)) {
			atomic.AddInt64(&tl.stats.trapsFiltered, 1)
			tl.logger.Debug().
				Str("source", sourceIP).
				Str("community", string(packet.Community)).
				Msg("Trap filtered by community string")
			return
		}
	}
	
	// Apply enterprise filtering
	if len(tl.config.Filters.AllowedEnterprises) > 0 {
		if !IsAllowedEnterprise(packet.Enterprise, tl.config.Filters.AllowedEnterprises) {
			atomic.AddInt64(&tl.stats.trapsFiltered, 1)
			tl.logger.Debug().
				Str("source", sourceIP).
				Str("enterprise", packet.Enterprise).
				Msg("Trap filtered by enterprise OID")
			return
		}
	}
	
	// Call the handler
	tl.logger.Debug().
		Str("source", sourceAddr).
		Msg("Calling trap handler")
	
	if tl.handler != nil {
		tl.handler(packet, sourceAddr)
	} else {
		tl.logger.Error().Msg("Trap handler is nil!")
	}
}


// isSourceAllowed checks if a source IP is allowed
func (tl *TrapListener) isSourceAllowed(ip string) bool {
	return isIPAllowed(ip, tl.config.Filters.AllowedSources, tl.config.Filters.BlockedSources)
}

// isCommunityAllowed checks if a community string is allowed
func (tl *TrapListener) isCommunityAllowed(community string) bool {
	if len(tl.config.Communities) == 0 {
		// No community filter means all are allowed
		return true
	}
	
	for _, allowed := range tl.config.Communities {
		if community == allowed {
			return true
		}
	}
	
	return false
}

// maintenanceLoop performs periodic maintenance tasks
func (tl *TrapListener) maintenanceLoop() {
	defer tl.wg.Done()
	
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Clean up rate limiter
			tl.rateLimiter.Cleanup()
			
			// Statistics are logged by the probe itself, skip here to avoid duplication
			
		case <-tl.shutdownCh:
			tl.logger.Debug().Msg("Trap listener maintenance loop shutting down")
			return
		}
	}
}

// getVersionString returns SNMP version as string
func getVersionString(version gosnmp.SnmpVersion) string {
	switch version {
	case gosnmp.Version1:
		return "SNMPv1"
	case gosnmp.Version2c:
		return "SNMPv2c"
	case gosnmp.Version3:
		return "SNMPv3"
	default:
		return "Unknown"
	}
}

// snmpLogger adapts our logger to gosnmp's logger interface
type snmpLogger struct {
	logger *logger.Logger
}

func (sl *snmpLogger) Print(v ...interface{}) {
	sl.logger.Debug().Interface("data", v).Msg("gosnmp")
}

func (sl *snmpLogger) Printf(format string, v ...interface{}) {
	sl.logger.Debug().Msgf("gosnmp: "+format, v...)
}