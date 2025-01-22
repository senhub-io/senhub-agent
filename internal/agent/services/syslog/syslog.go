// internal/agent/services/syslog/syslog.go
package syslog

import (
	"context"
	"fmt"
	"sync"
	"time"

	gsyslog "gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/mcuadros/go-syslog.v2/format"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type SyslogService interface {
	Start() error
	Stop(context.Context) error
	AddHandler(func(data_store.DataPoint) error)
}

type syslogService struct {
	port         int
	labels       map[string]string
	server       *gsyslog.Server
	messagesChan chan format.LogParts
	handlers     []func(data_store.DataPoint) error
	logger       *logger.Logger
	mutex        sync.RWMutex
}

func NewSyslogService(port int, labels map[string]string, logger *logger.Logger) SyslogService {
	localLogger := logger.With().Str("service", "SyslogService").Logger()
	return &syslogService{
		port:         port,
		labels:       labels,
		messagesChan: make(chan format.LogParts, 1000),
		handlers:     make([]func(data_store.DataPoint) error, 0),
		logger:       &localLogger,
	}
}

func (s *syslogService) Start() error {
	server := gsyslog.NewServer()
	server.SetFormat(gsyslog.RFC5424)
	server.SetHandler(gsyslog.NewChannelHandler(s.messagesChan))

	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	if err := server.ListenUDP(addr); err != nil {
		return fmt.Errorf("failed to start syslog server: %w", err)
	}

	if err := server.Boot(); err != nil {
		return fmt.Errorf("failed to boot syslog server: %w", err)
	}

	s.server = server

	go s.processMessages()

	s.logger.Info().Msgf("Syslog server started on port %d", s.port)
	return nil
}

func (s *syslogService) Stop(ctx context.Context) error {
	if s.server != nil {
		if err := s.server.Kill(); err != nil {
			return fmt.Errorf("error shutting down syslog server: %w", err)
		}
	}
	return nil
}

func (s *syslogService) AddHandler(handler func(data_store.DataPoint) error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.handlers = append(s.handlers, handler)
}

func (s *syslogService) convertLogToDataPoint(logParts format.LogParts) data_store.DataPoint {
	logTags := []tags.Tag{
		{Key: "facility", Value: fmt.Sprintf("%v", logParts["facility"]), Private: false},
		{Key: "severity", Value: fmt.Sprintf("%v", logParts["severity"]), Private: false},
		{Key: "hostname", Value: fmt.Sprintf("%v", logParts["hostname"]), Private: false},
		{Key: "app_name", Value: fmt.Sprintf("%v", logParts["app_name"]), Private: false},
		{Key: "message", Value: fmt.Sprintf("%v", logParts["message"]), Private: false},
	}

	for k, v := range s.labels {
		logTags = append(logTags, tags.Tag{Key: k, Value: v, Private: false})
	}

	timestamp := time.Now()
	if ts, ok := logParts["timestamp"].(time.Time); ok {
		timestamp = ts
	}

	return data_store.DataPoint{
		Name:      "syslog",
		Timestamp: timestamp,
		Value:     1.0,
		Tags:      logTags,
	}
}

func (s *syslogService) processMessages() {
	for logParts := range s.messagesChan {
		// Ignorer les messages si tous les champs importants sont vides ou 0
		if logParts["facility"] == "0" &&
			logParts["severity"] == "0" &&
			(logParts["hostname"] == "" || logParts["hostname"] == nil) &&
			(logParts["app_name"] == "" || logParts["app_name"] == nil) &&
			(logParts["message"] == "" || logParts["message"] == nil) {
			continue
		}

		dataPoint := s.convertLogToDataPoint(logParts)

		// Ajouter l'IP source comme tag
		srcIP := ""
		if addr, ok := logParts["client_address"].(string); ok && addr != "" {
			srcIP = addr
		}

		dataPoint.Tags = append(dataPoint.Tags, tags.Tag{
			Key:     "source_ip",
			Value:   srcIP,
			Private: false,
		})

		// On log uniquement les messages qui ont passé le filtre
		fmt.Printf("Received syslog message from %s: facility=%v severity=%v hostname=%v app=%v message=%v\n",
			srcIP,
			logParts["facility"],
			logParts["severity"],
			logParts["hostname"],
			logParts["app_name"],
			logParts["message"])

		s.mutex.RLock()
		for _, handler := range s.handlers {
			if err := handler(dataPoint); err != nil {
				fmt.Printf("Failed to process log message: %v\n", err)
			}
		}
		s.mutex.RUnlock()
	}
}
