// senhub-agent/internal/agent/probes/syslog/syslogProbe.go
package syslog

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/mcuadros/go-syslog.v2"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/utils/netbind"
)

const (
	DefaultPort     = 514
	DefaultProtocol = "udp"
	// DefaultBindAddress is loopback-only (#278): the listener has no
	// authentication, so receiving syslog from remote senders requires
	// an explicit `bind_address` opt-in. The address was previously
	// hardcoded to 0.0.0.0 with no way to restrict it.
	DefaultBindAddress  = "127.0.0.1"
	DefaultSyncInterval = 30 * time.Second
	MinPort             = 1
	MaxPort             = 65535
)

type SyslogProbeConfig struct {
	Port        int
	Protocol    string
	BindAddress string
}

type SyslogProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       SyslogProbeConfig
	moduleLogger *logger.ModuleLogger
	server       *syslog.Server
	callback     func([]data_store.DataPoint) error
}

func (p *SyslogProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}

func NewSyslogProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	// Create module-specific logger for syslog probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.syslog")

	parsedConfig, err := parseSyslogProbeConfig(config)
	if err != nil {
		return nil, err
	}

	moduleLogger.Debug().
		Any("config", parsedConfig).
		Msg("Creating new syslog probe")

	return &SyslogProbe{
		BaseProbe:    &types.BaseProbe{},
		rawConfig:    config,
		config:       parsedConfig,
		moduleLogger: moduleLogger,
	}, nil
}

func parseSyslogProbeConfig(config map[string]interface{}) (SyslogProbeConfig, error) {
	errs := []error{}
	var port int = DefaultPort
	var protocol string = DefaultProtocol
	var bindAddress string = DefaultBindAddress

	if v, ok := types.IntParam(config, "port"); ok {
		port = v
		if port < MinPort || port > MaxPort {
			errs = append(errs, fmt.Errorf("port must be between %d and %d", MinPort, MaxPort))
		}
	}

	if protocolVal, ok := config["protocol"].(string); ok {
		protocol = protocolVal
		if protocol != "tcp" && protocol != "udp" {
			errs = append(errs, fmt.Errorf("protocol must be 'tcp' or 'udp'"))
		}
	}

	if v, ok := config["bind_address"].(string); ok && v != "" {
		bindAddress = v
	}

	if len(errs) > 0 {
		return SyslogProbeConfig{}, fmt.Errorf("error parsing config: %v", errs)
	}

	return SyslogProbeConfig{
		Port:        port,
		Protocol:    protocol,
		BindAddress: bindAddress,
	}, nil
}

func (p *SyslogProbe) GetTargetStrategies() []string {
	return []string{"event"}
}

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "syslog", "syslog2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

func (p *SyslogProbe) ShouldStart() bool {
	return true
}

func (p *SyslogProbe) GetInterval() time.Duration {
	return DefaultSyncInterval
}

func (p *SyslogProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, nil // Event-driven, pas de collection périodique
}

func (p *SyslogProbe) OnStart(quitChannel chan struct{}) error {
	p.moduleLogger.Info().
		Str("protocol", p.config.Protocol).
		Str("bind_address", p.config.BindAddress).
		Int("port", p.config.Port).
		Msg("Starting syslog probe")

	if netbind.IsWildcard(p.config.BindAddress) {
		p.moduleLogger.Warn().
			Str("bind_address", p.config.BindAddress).
			Msg("Syslog listener bound to ALL interfaces without authentication — restrict `bind_address` or firewall the port")
	}

	channel := make(syslog.LogPartsChannel)
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.Automatic)
	server.SetHandler(handler)

	address := fmt.Sprintf("%s:%d", p.config.BindAddress, p.config.Port)
	switch p.config.Protocol {
	case "udp":
		if err := server.ListenUDP(address); err != nil {
			return fmt.Errorf("failed to start UDP listener: %w", err)
		}
	case "tcp":
		if err := server.ListenTCP(address); err != nil {
			return fmt.Errorf("failed to start TCP listener: %w", err)
		}
	}

	if err := server.Boot(); err != nil {
		return fmt.Errorf("failed to start syslog server: %w", err)
	}

	p.server = server
	p.moduleLogger.Info().Msg("Syslog server started successfully")

	go func() {
		for {
			select {
			case logParts := <-channel:
				p.processLogMessage(logParts)
			case <-quitChannel:
				p.moduleLogger.Info().Msg("Received quit signal, stopping message processing")
				return
			}
		}
	}()

	return nil
}

func (p *SyslogProbe) OnShutdown(ctx context.Context) error {
	if p.server != nil {
		p.moduleLogger.Info().Msg("Stopping syslog probe")
		return p.server.Kill()
	}
	return nil
}

func (p *SyslogProbe) processLogMessage(logParts map[string]interface{}) {
	facility, _ := logParts["facility"].(int)
	severity, _ := logParts["severity"].(int)
	hostname, _ := logParts["hostname"].(string)
	client, _ := logParts["client"].(string)
	priority, _ := logParts["priority"].(int)
	timestamp, _ := logParts["timestamp"].(time.Time)

	// The server is configured with syslog.Automatic format
	// detection: per-message the library decides RFC3164 vs RFC5424
	// and populates either {content, tag} or {message, app_name}.
	// facility / severity / priority / hostname / client share the
	// same key names across both parsers (which is why they were the
	// only fields that survived for RFC5424 traffic pre-#135), but
	// the body and application name do not. Read the RFC3164 keys
	// first; fall back to RFC5424 keys when they are empty so a
	// mixed-traffic deployment (Ubuntu 24.04's `logger` defaults to
	// RFC5424, older clients still emit RFC3164) lands every body
	// in OTLP.
	content, _ := logParts["content"].(string)
	if content == "" {
		if v, ok := logParts["message"].(string); ok {
			content = v
		}
	}
	tag, _ := logParts["tag"].(string)
	if tag == "" {
		if v, ok := logParts["app_name"].(string); ok {
			tag = v
		}
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	eventTags := []tags.Tag{
		{Key: "facility", Value: fmt.Sprintf("%d", facility), Private: false},
		{Key: "severity", Value: fmt.Sprintf("%d", severity), Private: false},
		{Key: "host", Value: hostname, Private: false},
		{Key: "message", Value: content, Private: false},
		{Key: "tag", Value: tag, Private: false},
		{Key: "client", Value: client, Private: false},
		{Key: "priority", Value: fmt.Sprintf("%d", priority), Private: false},
	}

	p.moduleLogger.Debug().
		Int("facility", facility).
		Int("severity", severity).
		Str("host", hostname).
		Str("message", content).
		Msg("Received syslog message")

	if p.callback == nil {
		p.moduleLogger.Warn().Msg("Callback is not set")
		return
	}

	dataPoint := data_store.DataPoint{
		Name:      "syslog_event",
		Timestamp: timestamp,
		Value:     float64(severity),
		Tags:      eventTags,
	}

	p.moduleLogger.Debug().
		Time("timestamp", timestamp).
		Int("severity", severity).
		Msg("Sending DataPoint to DataStore")

	if err := p.callback([]data_store.DataPoint{dataPoint}); err != nil {
		p.moduleLogger.Error().
			Err(err).
			Msg("Failed to send DataPoint to DataStore")
	}

	// Also publish to the agent's log channel so the OTLP strategy
	// (and any future log sink) can ship the message as a structured
	// log record. Independent of the data_store routing — the syslog
	// message is a log, not a metric, even though the existing event
	// strategy sees it as a DataPoint.
	agentstate.PublishLog(agentstate.LogRecord{
		Timestamp:    timestamp,
		Severity:     agentstate.SyslogPriorityToSeverity(severity),
		SeverityText: agentstate.SyslogPriorityToText(severity),
		Body:         content,
		Attributes: map[string]string{
			"syslog.facility":      fmt.Sprintf("%d", facility),
			"syslog.severity_code": fmt.Sprintf("%d", severity),
			"syslog.priority":      fmt.Sprintf("%d", priority),
			"syslog.hostname":      hostname,
			"syslog.appname":       tag,
			"syslog.client":        client,
		},
		ProducerProbeName: p.GetName(),
		ProducerProbeType: "syslog",
	})
}

func (p *SyslogProbe) String() string {
	return fmt.Sprintf("SyslogProbe{protocol=%s, bind=%s, port=%d}", p.config.Protocol, p.config.BindAddress, p.config.Port)
}
