package snmptrap

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// SNMPTrapProbe collects SNMP traps from network devices
type SNMPTrapProbe struct {
	config       *Config
	listener     *TrapListener
	buffer       *TrapBuffer
	enricher     *TrapEnricher
	mibManager   *MIBManager
	enterprises  map[string]EnterpriseInfo
	running      bool
	shutdownCh   chan struct{}
	wg           sync.WaitGroup
	mutex        sync.RWMutex
	logger       *logger.Logger
	moduleLogger *logger.ModuleLogger
	callback     func([]data_store.DataPoint) error
	
	// Metrics for monitoring
	stats struct {
		trapsReceived   int64
		trapsProcessed  int64
		trapsDropped    int64
		lastTrapTime    time.Time
		startTime       time.Time
	}
}

// NewSNMPTrapProbe creates a new SNMP Trap probe instance
func NewSNMPTrapProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	// Create module-specific logger
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.snmptrap")
	
	// Parse configuration
	parsedConfig, err := parseConfig(config)
	if err != nil {
		moduleLogger.Error().Err(err).Msg("Failed to parse SNMP trap probe configuration")
		return nil, err
	}
	
	moduleLogger.Info().
		Str("listen_address", parsedConfig.ListenAddress).
		Int("buffer_size", parsedConfig.BufferSize).
		Bool("mib_enrichment", parsedConfig.MIBEnrichment.Enabled).
		Msg("Initializing SNMP Trap probe")
	
	// Initialize probe
	probe := &SNMPTrapProbe{
		config:       parsedConfig,
		shutdownCh:   make(chan struct{}),
		enterprises:  KnownEnterprises,
		logger:       baseLogger,
		moduleLogger: moduleLogger,
	}
	
	// Initialize components
	probe.buffer = NewTrapBuffer(parsedConfig.BufferSize, baseLogger)
	probe.mibManager = NewMIBManager(parsedConfig.MIBEnrichment, baseLogger)
	probe.enricher = NewTrapEnricher(probe.mibManager, probe.enterprises, baseLogger)
	
	// Create trap listener
	probe.listener = NewTrapListener(parsedConfig, probe.handleTrap, baseLogger)
	
	probe.stats.startTime = time.Now()
	
	return probe, nil
}

// GetName returns the probe name
func (p *SNMPTrapProbe) GetName() string {
	return "snmptrap"
}

// ShouldStart indicates if probe should be activated based on environment
func (p *SNMPTrapProbe) ShouldStart() bool {
	return true // SNMP Trap probe can run on any environment
}

// GetInterval returns the collection frequency (not used for event-driven probes)
func (p *SNMPTrapProbe) GetInterval() time.Duration {
	return 0 // Event-driven, no periodic collection
}

// GetTargetStrategies returns the strategies this probe should send data to
func (p *SNMPTrapProbe) GetTargetStrategies() []string {
	return []string{"event"}
}

// Collect returns collected datapoints (not used for event-driven probes)
func (p *SNMPTrapProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, nil // Event-driven, no periodic collection
}

// SetCallback sets the callback function for sending datapoints
func (p *SNMPTrapProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}

// OnStart initializes the probe and starts listening for traps
func (p *SNMPTrapProbe) OnStart(quitChannel chan struct{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if p.running {
		p.moduleLogger.Warn().Msg("Probe already running")
		return nil
	}
	
	p.moduleLogger.Info().Msg("Starting SNMP Trap probe")
	
	// Load MIBs if enrichment is enabled
	if p.config.MIBEnrichment.Enabled {
		p.moduleLogger.Debug().
			Str("external_mibs_path", p.config.MIBEnrichment.ExternalMIBsPath).
			Msg("Loading MIBs for enrichment")
		if err := p.mibManager.LoadMIBs(); err != nil {
			p.moduleLogger.Warn().Err(err).Msg("Failed to load some MIBs, continuing with available MIBs")
		}
		
		stats := p.mibManager.GetStats()
		p.moduleLogger.Info().
			Int("loaded_mibs", stats.LoadedMIBCount).
			Int("cache_size", stats.CacheSize).
			Str("external_mibs_path", p.config.MIBEnrichment.ExternalMIBsPath).
			Msg("MIB loading completed")
	}
	
	// Start the listener
	p.moduleLogger.Debug().
		Str("address", p.config.ListenAddress).
		Msg("Attempting to start trap listener")
		
	if err := p.listener.Start(); err != nil {
		p.moduleLogger.Error().
			Err(err).
			Str("address", p.config.ListenAddress).
			Msg("Failed to start trap listener")
		return err
	}
	
	p.running = true
	
	// Start background tasks
	p.wg.Add(1)
	go p.maintenanceLoop()
	
	p.moduleLogger.Info().
		Str("address", p.config.ListenAddress).
		Msg("SNMP Trap probe started successfully")
	
	return nil
}

// convertTrapToDataPoint converts an enriched trap to a datapoint
func (p *SNMPTrapProbe) convertTrapToDataPoint(trap *EnrichedTrap) data_store.DataPoint {
	// Convert tags from map to []tags.Tag
	tagsList := []tags.Tag{
		{Key: "host", Value: trap.SourceHost},  // Required by event strategy
		{Key: "trap_oid", Value: trap.TrapOID},
		{Key: "trap_name", Value: trap.TrapName},
		{Key: "enterprise", Value: trap.Enterprise},
		{Key: "enterprise_full", Value: trap.EnterpriseFull},
		{Key: "category", Value: trap.Category},
		{Key: "severity", Value: trap.Severity},
		{Key: "event_type", Value: "snmp_trap"},
		{Key: "message", Value: trap.Message},
	}
	
	return data_store.DataPoint{
		Name:      "snmp_trap_event",
		Value:     1.0,
		Timestamp: trap.Timestamp,
		Tags:      tagsList,
	}
}

// OnShutdown gracefully stops the probe
func (p *SNMPTrapProbe) OnShutdown(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if !p.running {
		return nil
	}
	
	p.moduleLogger.Info().Msg("Shutting down SNMP Trap probe")
	
	// Signal shutdown
	close(p.shutdownCh)
	
	// Stop listener
	if err := p.listener.Stop(); err != nil {
		p.moduleLogger.Error().Err(err).Msg("Error stopping trap listener")
	}
	
	// Wait for background tasks with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		p.moduleLogger.Debug().Msg("All background tasks stopped")
	case <-ctx.Done():
		p.moduleLogger.Warn().Msg("Shutdown timeout, forcing stop")
	}
	
	// Flush remaining traps
	remaining := p.buffer.Flush()
	if len(remaining) > 0 {
		p.moduleLogger.Warn().
			Int("count", len(remaining)).
			Msg("Dropping remaining traps due to shutdown")
		p.stats.trapsDropped += int64(len(remaining))
	}
	
	p.running = false
	
	// Log final statistics
	p.logFinalStats()
	
	p.moduleLogger.Info().Msg("SNMP Trap probe shutdown complete")
	return nil
}

// handleTrap processes incoming SNMP traps
func (p *SNMPTrapProbe) handleTrap(packet *gosnmp.SnmpPacket, addr string) {
	p.stats.trapsReceived++
	p.stats.lastTrapTime = time.Now()
	
	p.moduleLogger.Debug().
		Str("source", addr).
		Str("oid", packet.Enterprise).
		Int("varbind_count", len(packet.Variables)).
		Msg("Received SNMP trap")
	
	// Parse the trap
	parsedTrap := p.parseTrap(packet, addr)
	
	// Enrich with MIB data if enabled
	var enrichedTrap *EnrichedTrap
	if p.config.MIBEnrichment.Enabled {
		enrichedTrap = p.enricher.Enrich(parsedTrap)
	} else {
		enrichedTrap = p.enricher.BasicEnrich(parsedTrap)
	}
	
	// Convert to datapoint and send via callback
	if p.callback != nil {
		dataPoint := p.convertTrapToDataPoint(enrichedTrap)
		
		// Log enriched trap details
		p.moduleLogger.Info().
			Str("trap_oid", enrichedTrap.TrapOID).
			Str("trap_name", enrichedTrap.TrapName).
			Str("enterprise", enrichedTrap.Enterprise).
			Str("enterprise_full", enrichedTrap.EnterpriseFull).
			Str("category", enrichedTrap.Category).
			Str("severity", enrichedTrap.Severity).
			Str("message", enrichedTrap.Message).
			Str("source", enrichedTrap.SourceHost).
			Msg("Trap enrichment result")
		
		p.moduleLogger.Debug().
			Str("name", dataPoint.Name).
			Float32("value", dataPoint.Value).
			Int("tags", len(dataPoint.Tags)).
			Msg("Sending trap to callback")
		
		if err := p.callback([]data_store.DataPoint{dataPoint}); err != nil {
			p.stats.trapsDropped++
			p.moduleLogger.Error().
				Err(err).
				Str("source", addr).
				Str("trap_oid", enrichedTrap.TrapOID).
				Msg("Failed to send trap via callback")
		} else {
			p.stats.trapsProcessed++
			p.moduleLogger.Debug().
				Str("trap_oid", enrichedTrap.TrapOID).
				Msg("Trap sent successfully via callback")
		}
	} else {
		p.moduleLogger.Warn().Msg("No callback set - trap will be buffered")
		// Fallback: Add to buffer if no callback is set
		if !p.buffer.Add(enrichedTrap) {
			p.stats.trapsDropped++
			p.moduleLogger.Warn().
				Str("source", addr).
				Str("trap_oid", enrichedTrap.TrapOID).
				Msg("Buffer full, dropping trap")
		}
	}
}

// parseTrap converts raw SNMP trap to internal format
func (p *SNMPTrapProbe) parseTrap(packet *gosnmp.SnmpPacket, addr string) *ParsedTrap {
	// Extract source IP
	sourceIP := extractIP(addr)
	
	p.moduleLogger.Debug().
		Str("source", addr).
		Int("version", int(packet.Version)).
		Int("generic_trap", packet.GenericTrap).
		Int("specific_trap", packet.SpecificTrap).
		Str("enterprise", packet.Enterprise).
		Int("varbind_count", len(packet.Variables)).
		Msg("Parsing SNMP trap")
	
	// Build trap OID based on version
	var trapOID string
	
	// Check if it's SNMPv2c/v3 by looking for the standard trap OID varbind
	if len(packet.Variables) > 1 && packet.Variables[1].Name == ".1.3.6.1.6.3.1.1.4.1.0" {
		// SNMPv2c/v3 format - trap OID is in second varbind
		trapOID = fmt.Sprint(packet.Variables[1].Value)
		p.moduleLogger.Debug().
			Str("trap_oid", trapOID).
			Str("varbind_oid", packet.Variables[1].Name).
			Interface("varbind_value", packet.Variables[1].Value).
			Msg("Parsed SNMPv2c/v3 trap OID")
	} else if packet.Enterprise != "" && packet.SpecificTrap >= 0 {
		// SNMPv1 format
		trapOID = fmt.Sprintf("%s.0.%d", packet.Enterprise, packet.SpecificTrap)
		p.moduleLogger.Debug().
			Str("trap_oid", trapOID).
			Msg("Parsed SNMPv1 trap OID")
	} else {
		p.moduleLogger.Debug().
			Int("varbind_count", len(packet.Variables)).
			Msg("Could not determine trap OID format")
		if len(packet.Variables) > 0 {
			for i, v := range packet.Variables {
				p.moduleLogger.Debug().
					Int("index", i).
					Str("oid", v.Name).
					Interface("value", v.Value).
					Msg("Varbind details")
			}
		}
	}
	
	// Parse varbinds
	varbinds := make([]Varbind, 0, len(packet.Variables))
	for _, v := range packet.Variables {
		varbind := Varbind{
			OID:   v.Name,
			Type:  getTypeString(v.Type),
			Value: v.Value,
		}
		varbinds = append(varbinds, varbind)
	}
	
	return &ParsedTrap{
		Timestamp:    time.Now(),
		SourceIP:     sourceIP,
		TrapOID:      trapOID,
		EnterpriseOID: packet.Enterprise,
		GenericTrap:  packet.GenericTrap,
		SpecificTrap: packet.SpecificTrap,
		Varbinds:     varbinds,
		Version:      packet.Version,
		Community:    packet.Community,
	}
}

// maintenanceLoop performs periodic maintenance tasks
func (p *SNMPTrapProbe) maintenanceLoop() {
	defer p.wg.Done()
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Log statistics
			p.logStats()
			
			// Clean cache if needed
			if p.config.MIBEnrichment.Enabled {
				p.mibManager.CleanCache()
			}
			
		case <-p.shutdownCh:
			p.moduleLogger.Debug().Msg("Maintenance loop shutting down")
			return
		}
	}
}

// logStats logs current probe statistics
func (p *SNMPTrapProbe) logStats() {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	if p.stats.trapsReceived > 0 || time.Since(p.stats.startTime) > 5*time.Minute {
		bufferStats := p.buffer.GetStats()
		
		p.moduleLogger.Info().
			Int64("received", p.stats.trapsReceived).
			Int64("processed", p.stats.trapsProcessed).
			Int64("dropped", p.stats.trapsDropped).
			Int("buffer_used", bufferStats.CurrentSize).
			Int("buffer_capacity", bufferStats.Capacity).
			Str("uptime", time.Since(p.stats.startTime).Round(time.Second).String()).
			Str("last_trap", p.stats.lastTrapTime.Format("15:04:05")).
			Msg("SNMP Trap statistics")
	}
}

// logFinalStats logs final statistics on shutdown
func (p *SNMPTrapProbe) logFinalStats() {
	uptime := time.Since(p.stats.startTime)
	
	p.moduleLogger.Info().
		Int64("total_traps_received", p.stats.trapsReceived).
		Int64("total_traps_processed", p.stats.trapsProcessed).
		Int64("total_traps_dropped", p.stats.trapsDropped).
		Str("uptime", uptime.String()).
		Float64("avg_traps_per_sec", float64(p.stats.trapsReceived)/uptime.Seconds()).
		Msg("SNMP Trap probe final statistics")
}

