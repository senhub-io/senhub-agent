// internal/agent/probes/syslog/syslogProbe.go
package syslog

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/syslog"
)

const (
	DefaultSyslogPort = 514
	MinPort           = 1
	MaxPort           = 65535
)

type SyslogProbeConfig struct {
	Port   int
	Labels map[string]string
}

type SyslogProbe struct {
	config   SyslogProbeConfig
	logger   *logger.Logger
	service  syslog.SyslogService
	callback func([]data_store.DataPoint) error
}

func NewSyslogProbe(config map[string]interface{}, logger *logger.Logger) (types.Probe, error) {
	parsedConfig, err := parseSyslogProbeConfig(config)
	if err != nil {
		return nil, err
	}

	service := syslog.NewSyslogService(
		parsedConfig.Port,
		parsedConfig.Labels,
		logger,
	)

	return &SyslogProbe{
		config:  parsedConfig,
		logger:  logger,
		service: service,
	}, nil
}

func parseSyslogProbeConfig(config map[string]interface{}) (SyslogProbeConfig, error) {
	port := DefaultSyslogPort
	if portVal, ok := config["port"].(float64); ok {
		port = int(portVal)
		if port < MinPort || port > MaxPort {
			return SyslogProbeConfig{}, fmt.Errorf("port must be between %d and %d", MinPort, MaxPort)
		}
	}

	labels := make(map[string]string)
	if labelsVal, ok := config["labels"].(map[string]interface{}); ok {
		for k, v := range labelsVal {
			if strVal, ok := v.(string); ok {
				labels[k] = strVal
			}
		}
	}

	return SyslogProbeConfig{
		Port:   port,
		Labels: labels,
	}, nil
}

func (p *SyslogProbe) GetName() string {
	return "syslogProbe"
}

func (p *SyslogProbe) ShouldStart() bool {
	return true
}

func (p *SyslogProbe) GetInterval() time.Duration {
	return 24 * time.Hour // Un long intervalle car nous n'utilisons pas le polling
}

func (p *SyslogProbe) Collect() ([]data_store.DataPoint, error) {
	return nil, nil // La collecte est gérée par le service Syslog
}

func (p *SyslogProbe) OnStart(quitChannel chan struct{}) error {
	if p.callback == nil {
		p.logger.Error().Msg("DataStore callback not set")
		return fmt.Errorf("datastore callback not set")
	}

	p.service.AddHandler(func(dp data_store.DataPoint) error {
		return p.callback([]data_store.DataPoint{dp})
	})

	return p.service.Start()
}

func (p *SyslogProbe) OnShutdown(ctx context.Context) error {
	return p.service.Stop(ctx)
}

func (p *SyslogProbe) SetCallback(callback func([]data_store.DataPoint) error) {
	p.callback = callback
}
