package zabbix

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// Strategy is the Zabbix active-agent output.
//
// Its loop has three clocks: the check list is refreshed every
// RefreshInterval (the first request is also what registers the host),
// the latest value of every requested item is pushed every Interval, and
// a heartbeat goes out every HeartbeatInterval so the server keeps the
// host available between pushes.
type Strategy struct {
	rawParams configuration.StorageConfigParams
	cfg       Config
	logger    *logger.ModuleLogger
	defs      otelmapper.DefinitionLookup

	store   *store
	client  *client
	passive *passiveListener

	mu        sync.Mutex
	started   bool
	cancel    context.CancelFunc
	done      chan struct{}
	requested map[string]struct{}
	// heartbeatOff is set once the server refused the heartbeat request
	// (servers before 6.2), so the loop stops sending it.
	heartbeatOff bool
}

// New builds the strategy; the configuration is parsed by
// ValidateConfigParams, which the store calls before Start.
func New(params configuration.StorageConfigParams, baseLogger *logger.Logger, defs otelmapper.DefinitionLookup) *Strategy {
	return &Strategy{
		rawParams: params,
		logger:    logger.NewModuleLogger(baseLogger, "strategy.zabbix"),
		defs:      defs,
		store:     newStore(),
		requested: map[string]struct{}{},
	}
}

func (s *Strategy) GetStrategyName() string { return "zabbix" }

// ConsumesOtelMetrics tells the store to route to this output what it
// routes to the OTLP output: the probes name their targets in a fixed
// list that predates this sink.
func (s *Strategy) ConsumesOtelMetrics() bool { return true }

func (s *Strategy) GetStrategyParams() map[string]interface{} { return s.rawParams }

func (s *Strategy) ValidateConfigParams(params configuration.StorageConfigParams) error {
	cfg, err := ParseConfig(params)
	if err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

func (s *Strategy) AddDataPoints(data []datapoint.DataPoint) error {
	for _, dp := range data {
		s.store.upsert(dp)
	}
	return nil
}

func (s *Strategy) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	if s.cfg.Server == "" {
		return fmt.Errorf("zabbix: configuration not validated before start")
	}
	c, err := newClient(s.cfg)
	if err != nil {
		return err
	}
	s.client = c
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	if s.cfg.Passive.Enabled {
		pl, err := newPassiveListener(s.cfg, s.valueOf, s.logger)
		if err != nil {
			cancel()
			return err
		}
		if err := pl.start(runCtx); err != nil {
			cancel()
			return err
		}
		s.passive = pl
		s.logger.Info().Str("listen", pl.addr()).Msg("Zabbix passive listener started")
	}
	s.cancel = cancel
	s.done = make(chan struct{})
	s.started = true
	go s.run(runCtx)
	s.logger.Info().
		Str("server", strings.Join(s.cfg.addresses(), ",")).
		Str("hostname", s.cfg.Hostname).
		Dur("interval", s.cfg.Interval).
		Msg("Zabbix active agent started")
	return nil
}

// valueOf serves the passive listener: the current value of one key,
// as the active push would send it.
func (s *Strategy) valueOf(key string) (string, bool) {
	for _, it := range s.items(time.Now()) {
		if it.Key == key {
			return it.Value, true
		}
	}
	return "", false
}

func (s *Strategy) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	cancel, done, passive := s.cancel, s.done, s.passive
	s.passive = nil
	s.mu.Unlock()

	cancel()
	if passive != nil {
		passive.stop()
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.logger.Info().Msg("Zabbix active agent stopped")
	return nil
}

func (s *Strategy) run(ctx context.Context) {
	defer close(s.done)

	s.refresh(ctx)

	push := time.NewTicker(s.cfg.Interval)
	refresh := time.NewTicker(s.cfg.RefreshInterval)
	heartbeat := time.NewTicker(s.cfg.HeartbeatInterval)
	defer push.Stop()
	defer refresh.Stop()
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			s.refresh(ctx)
		case <-push.C:
			s.push(ctx)
		case <-heartbeat.C:
			s.beat(ctx)
		}
	}
}

// refresh asks the server which items it wants. Until the host exists on
// the server and a template gives it items, the list is empty and
// nothing is pushed; the values are ready the moment the list arrives.
func (s *Strategy) refresh(ctx context.Context) {
	items, err := s.client.activeChecks(ctx)
	if errors.Is(err, errHostUnknown) {
		// The reply a server gives on first contact: the request itself
		// fired the autoregistration event, and the host exists on the
		// next refresh once the action has run.
		s.logger.Info().
			Str("hostname", s.cfg.Hostname).
			Msg("Host not known to the server yet; waiting for its autoregistration")
		return
	}
	if err != nil {
		s.logger.Warn().Err(err).Msg("Check list request failed")
		agentstate.RecordExportFailure("zabbix", err.Error())
		return
	}
	requested := make(map[string]struct{}, len(items))
	for _, it := range items {
		requested[it.Key] = struct{}{}
	}
	s.mu.Lock()
	s.requested = requested
	s.mu.Unlock()
	s.logger.Debug().Int("items", len(items)).Msg("Check list refreshed")
	if len(items) == 0 {
		s.logger.Info().
			Str("hostname", s.cfg.Hostname).
			Msg("The server asks for no item yet: the host may still be waiting for its autoregistration action or a template")
	}
}

// push sends the latest value of every requested item.
func (s *Strategy) push(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	requested := s.requested
	s.mu.Unlock()

	available := s.items(now)
	if len(requested) == 0 {
		s.logger.Debug().Int("available", len(available)).Msg("No item requested, nothing pushed")
		return
	}
	batch := make([]item, 0, len(available))
	for _, it := range available {
		if _, ok := requested[it.Key]; ok {
			batch = append(batch, it)
		}
	}
	if len(batch) == 0 {
		sample := make([]string, 0, 10)
		for _, it := range available {
			if len(sample) == cap(sample) {
				break
			}
			sample = append(sample, it.Key)
		}
		wanted := make([]string, 0, 10)
		for k := range requested {
			if len(wanted) == cap(wanted) {
				break
			}
			wanted = append(wanted, k)
		}
		s.logger.Debug().
			Int("requested", len(requested)).
			Int("available", len(available)).
			Strs("requested_sample", wanted).
			Strs("available_sample", sample).
			Msg("None of the requested items is collected here")
		return
	}

	res, err := s.client.sendValues(ctx, batch, now)
	if err != nil {
		s.logger.Warn().Err(err).Int("items", len(batch)).Msg("Push failed")
		agentstate.RecordExportFailure("zabbix", err.Error())
		return
	}
	agentstate.RecordExportSuccess("zabbix")
	s.logger.Debug().
		Int("sent", len(batch)).
		Int("processed", res.Processed).
		Int("failed", res.Failed).
		Msg("Batch pushed")
}

// items renders the current series as Zabbix items, dropping the series
// not collected for three push intervals.
func (s *Strategy) items(now time.Time) []item {
	metrics := s.store.snapshot(now, 3*s.cfg.Interval)
	out := make([]item, 0, len(metrics))
	for _, cm := range metrics {
		out = append(out, itemFor(s.cfg.KeyPrefix, s.lookup(cm.ProbeType), cm))
	}
	return append(out, discoveryItems(s.cfg.KeyPrefix, s.defs, metrics)...)
}

func (s *Strategy) lookup(probeType string) *transformers.ProbeDefinition {
	if s.defs == nil {
		return nil
	}
	return s.defs.GetProbeDefinition(probeType)
}

func (s *Strategy) beat(ctx context.Context) {
	s.mu.Lock()
	off := s.heartbeatOff
	s.mu.Unlock()
	if off {
		return
	}
	if err := s.client.heartbeat(ctx); err != nil {
		s.logger.Debug().Err(err).Msg("Heartbeat not accepted; the server may predate 6.2, heartbeat disabled")
		s.mu.Lock()
		s.heartbeatOff = true
		s.mu.Unlock()
	}
}
