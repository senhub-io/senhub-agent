// senhub-agent/internal/agent/probes/syslog/syslogProbe.go
package syslog

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/mcuadros/go-syslog.v2"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

const (
	DefaultPort         = 514
	DefaultProtocol     = "udp"
	DefaultSyncInterval = 30 * time.Second
	MinPort             = 1
	MaxPort             = 65535
)

type SyslogProbeConfig struct {
	Port     int
	Protocol string
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

	if portVal, ok := config["port"].(float64); ok {
		port = int(portVal)
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

	if len(errs) > 0 {
		return SyslogProbeConfig{}, fmt.Errorf("error parsing config: %v", errs)
	}

	return SyslogProbeConfig{
		Port:     port,
		Protocol: protocol,
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
		Int("port", p.config.Port).
		Msg("Starting syslog probe")

	channel := make(syslog.LogPartsChannel)
	handler := syslog.NewChannelHandler(channel)

	server := syslog.NewServer()
	server.SetFormat(syslog.Automatic)
	server.SetHandler(handler)

	address := fmt.Sprintf("0.0.0.0:%d", p.config.Port)
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
		fmt.Printf("[INFO] Stopping syslog probe\n")
		return p.server.Kill()
	}
	return nil
}

func (p *SyslogProbe) processLogMessage(logParts map[string]interface{}) {
	facility, _ := logParts["facility"].(int)
	severity, _ := logParts["severity"].(int)
	content, _ := logParts["content"].(string)
	hostname, _ := logParts["hostname"].(string)
	tag, _ := logParts["tag"].(string)
	client, _ := logParts["client"].(string)
	priority, _ := logParts["priority"].(int)
	timestamp, _ := logParts["timestamp"].(time.Time)
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
		Value:     float32(severity),
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
}

func (p *SyslogProbe) String() string {
	return fmt.Sprintf("SyslogProbe{protocol=%s, port=%d}", p.config.Protocol, p.config.Port)
}
