// Package sensor handles the configuration and lifecycle of monitoring probes
package sensor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/probes"
	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/exporthook"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

// isValidProbeName checks if a probe name is safe for use in HTTP URLs;
// the rule lives with ProbeConfig so the configurator applies the same.
func isValidProbeName(name string) bool {
	return configuration.IsValidProbeName(name)
}

// Sensor manages the pool of running probes. It is an agent service:
// Start launches the configured probes under the caller's context and
// they run until that context is cancelled or Shutdown is called.
type Sensor interface {
	lifecycle.Service
}

type sensor struct {
	// mu serializes SyncConfiguration and Shutdown: config-change
	// callbacks are dispatched by EventNotifier on fresh goroutines, so
	// rapid config changes would otherwise mutate startedProbes and
	// license concurrently.
	mu            sync.Mutex
	startedProbes []*probes.ProbePoller
	// failedProbes holds the configured probes that could not be started,
	// by ID, retried on a timer so a credential fixed after the fact is
	// picked up without restarting the agent.
	failedProbes map[string]failedProbe
	// retryWake tells the retry goroutine a probe has just failed to
	// start, so it holds a timer only while there is something to retry.
	retryWake chan struct{}
	// runCtx is the lifecycle context the sensor was started with. It is
	// the cancellation root every probe started here inherits — including
	// the ones a config reload starts long after Start returned, which
	// used to be handed a nil quit channel and depended entirely on the
	// scheduler's internal stop path to ever terminate (#285).
	runCtx           context.Context
	addDataPoint     data_store.AddCallback
	configProvider   configuration.ConfigurationProvider
	moduleLogger     *logger.ModuleLogger
	licenseValidator license.Validator
	license          *license.License
}

// NewSensor creates a new Sensor instance
// failedProbe is a configured probe whose start failed, and why.
type failedProbe struct {
	config configuration.ProbeConfig
	reason string
	// noRetry is set when the target refused the credentials: retrying
	// on the timer would be a failed sign-on every interval, which locks
	// accounts on some targets. A reload or a restart still tries again.
	noRetry bool
}

// probeStartRetryInterval is how often a probe that failed to start is
// tried again. A password expires, a certificate lapses, a target is
// down at boot: each is fixed outside the agent, and the probe must come
// back by itself rather than wait for a restart nobody knows is needed.
var probeStartRetryInterval = 2 * time.Minute

// maxParallelProbeStarts bounds how many probes SyncConfiguration brings up
// at once. A slow probe start (os_updates took 1m55s on a Linux host) must
// not delay the OTLP receiver's listener behind it.
const maxParallelProbeStarts = 8

// slowStartNoticeDelay is when a still-running probe start is called out:
// the other probes are already running in parallel and are not waiting.
const slowStartNoticeDelay = 10 * time.Second

func NewSensor(
	addDataPoint data_store.AddCallback,
	configProvider configuration.ConfigurationProvider,
	baseLogger *logger.Logger,
) Sensor {
	// Create module-specific logger for sensor service
	moduleLogger := logger.NewModuleLogger(baseLogger, "sensor")

	// Initialize license validator with embedded RSA public key
	var licenseValidator license.Validator
	jwtValidator, err := license.GetDefaultValidator(7) // 7-day grace period
	if err != nil {
		moduleLogger.Error().
			Err(err).
			Msg("Failed to initialize license validator - entering safe mode (free tier only)")
		// SECURITY: No fallback to MockValidator in production
		// If RSA public key fails to load, agent runs in free tier mode only
		licenseValidator = nil
	} else {
		licenseValidator = jwtValidator
	}

	// Try to load and validate license from configuration
	var validatedLicense *license.License
	config := configProvider.GetConfiguration()
	agentKey := config.Agent.AuthenticationKey
	if config.Agent.License != "" && licenseValidator != nil {
		moduleLogger.Info().Msg("License token found in configuration, validating...")
		lic, err := licenseValidator.ValidateLicense(config.Agent.License)
		if err != nil {
			// A licence WAS configured and the validator rejected it: this is
			// an operator-visible misconfiguration (e.g. a legacy compact
			// SH-XXXX token), not the legit no-licence free tier. Fail loud so
			// monitoring alerts instead of the agent silently dropping every
			// paid probe (#486).
			agentstate.SetLicenseInvalid(agentstate.LicenseReasonValidationFailed)
			moduleLogger.Error().
				Err(err).
				Msg("Configured license token is INVALID and was rejected - all paid probes disabled, agent degraded to FREE TIER. Fix or remove the license to clear this alert (self-metric senhub.agent.license.invalid=1)")
		} else if agentKey != "" && !license.VerifyBinding(config.Agent.License, agentKey, lic) {
			agentstate.SetLicenseInvalid(agentstate.LicenseReasonBindingMismatch)
			moduleLogger.Error().
				Msg("Configured license is NOT bound to this agent key and was rejected - all paid probes disabled, agent degraded to FREE TIER (self-metric senhub.agent.license.invalid=1)")
		} else {
			validatedLicense = lic
			tierName := string(lic.Tier)
			moduleLogger.Info().
				Str("tier", tierName).
				Bool("expired", lic.IsExpired).
				Time("expires_at", lic.ExpiresAt).
				Str("platform", lic.Platform).
				Msg("License validated successfully")

			if lic.IsExpired {
				if licenseValidator.IsInGracePeriod(lic) {
					gracePeriodEnd := lic.ExpiresAt.Add(time.Duration(lic.GracePeriodDays) * 24 * time.Hour)
					moduleLogger.Warn().
						Time("grace_period_ends", gracePeriodEnd).
						Str("platform", lic.Platform).
						Time("delegation_expires_at", lic.DelegationExpiresAt).
						Msg("License expired but in grace period: renew the licence, or the platform's delegation, before the grace period ends")
				} else {
					agentstate.SetLicenseInvalid(agentstate.LicenseReasonExpiredNoGrace)
					moduleLogger.Error().Msg("License expired and grace period ended - all paid probes disabled, agent degraded to FREE TIER (self-metric senhub.agent.license.invalid=1)")
					validatedLicense = nil // Disable license if expired outside grace period
				}
			}

			if validatedLicense != nil {
				agentstate.ClearLicenseInvalid()
			}
		}
	} else {
		moduleLogger.Info().
			Int("paid_probe_types", len(license.KnownPaidProbes())).
			Msg("No license configured - free tier: every probe type runs except the paid ones")
	}

	publishLicense(licenseValidator, validatedLicense)
	return &sensor{
		startedProbes:    []*probes.ProbePoller{},
		failedProbes:     map[string]failedProbe{},
		retryWake:        make(chan struct{}, 1),
		addDataPoint:     addDataPoint,
		configProvider:   configProvider,
		moduleLogger:     moduleLogger,
		licenseValidator: licenseValidator,
		license:          validatedLicense,
	}
}

// publishLicense lets the export-path extensions ask what the licence
// allows. The closure holds this licence: the export path never waits on
// the sensor lock, and a new licence replaces it.
func publishLicense(v license.Validator, lic *license.License) {
	if v == nil {
		exporthook.SetAuthorizer(nil)
		return
	}
	exporthook.SetAuthorizer(func(feature string) bool { return v.IsProbeAuthorized(lic, feature) })
}

func (s *sensor) GetName() string {
	return "Sensor"
}

// SyncConfiguration synchronizes probes with current configuration.
// Safe for concurrent use: invocations are serialized, so back-to-back
// config events apply one after the other.
func (s *sensor) SyncConfiguration() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.moduleLogger.Info().Msg("Starting configuration synchronization")

	// Re-validate license from current configuration (may have been loaded after initial startup)
	config := s.configProvider.GetConfiguration()
	if s.license == nil && config.Agent.License != "" && s.licenseValidator != nil {
		lic, err := s.licenseValidator.ValidateLicense(config.Agent.License)
		if err != nil {
			agentstate.SetLicenseInvalid(agentstate.LicenseReasonValidationFailed)
			s.moduleLogger.Error().Err(err).Msg("Configured license token is INVALID and was rejected during configuration sync - agent staying on FREE TIER (self-metric senhub.agent.license.invalid=1)")
		} else if agentKey := config.Agent.AuthenticationKey; agentKey != "" && !license.VerifyBinding(config.Agent.License, agentKey, lic) {
			// Mirror the constructor's binding enforcement (see above). Without
			// it the initial binding_mismatch rejection is silently undone by
			// the first SyncConfiguration — which runs at boot — unlocking paid
			// probes for a licence not bound to this agent's authentication key.
			agentstate.SetLicenseInvalid(agentstate.LicenseReasonBindingMismatch)
			s.moduleLogger.Error().Msg("Configured license is NOT bound to this agent key and was rejected during configuration sync - agent staying on FREE TIER (self-metric senhub.agent.license.invalid=1)")
		} else {
			isExpired := lic.IsExpired && !s.licenseValidator.IsInGracePeriod(lic)
			if !isExpired {
				s.license = lic
				publishLicense(s.licenseValidator, lic)
				agentstate.ClearLicenseInvalid()
				s.moduleLogger.Info().
					Str("tier", string(lic.Tier)).
					Strs("probes", lic.AuthorizedProbes).
					Msg("License validated during configuration sync")
			} else {
				agentstate.SetLicenseInvalid(agentstate.LicenseReasonExpiredNoGrace)
				s.moduleLogger.Error().Msg("Configured license is expired and outside its grace period - agent staying on FREE TIER (self-metric senhub.agent.license.invalid=1)")
			}
		}
	}

	validProbeIds := []string{}
	probeConfigs := config.Probes

	s.moduleLogger.Info().
		Int("config_probes", len(probeConfigs)).
		Int("running_probes", len(s.startedProbes)).
		Msg("Configuration sync status")

	// Pre-validation: Check probe names
	seenNames := make(map[string]bool)
	for i, probeConfig := range probeConfigs {
		// Validate probe name is URL-safe (used in HTTP endpoints like /prtg/{name})
		if !isValidProbeName(probeConfig.Name) {
			s.moduleLogger.Error().
				Str("probe_name", probeConfig.Name).
				Msg("CONFIGURATION ERROR: Probe name must contain only letters, digits, hyphens and underscores (no spaces or special characters) — skipping probe")
			probeConfigs[i].Name = "" // Mark for skipping
			continue
		}
		if seenNames[probeConfig.Name] {
			s.moduleLogger.Warn().
				Str("probe_name", probeConfig.Name).
				Msg("CONFIGURATION ERROR: Duplicate probe name in configuration - only the first instance will be used")
		}
		seenNames[probeConfig.Name] = true
	}

	// Phase 1: decide which probes the configuration wants. Nothing is
	// started yet: a probe whose configuration changed gets a new id, and
	// its previous instance must release what it holds first. A listener
	// (syslog, snmp_trap, otlp_receiver) started beside its predecessor
	// found its port taken, failed, and the predecessor was then stopped,
	// leaving nothing listening until the retry two minutes later.
	processedNames := make(map[string]bool)
	type pendingStart struct {
		id     string
		config configuration.ProbeConfig
	}
	toStart := []pendingStart{}
	for _, probeConfig := range probeConfigs {
		// Skip probes with invalid names (marked in pre-validation)
		if probeConfig.Name == "" {
			continue
		}
		// Skip duplicates (already warned in pre-validation)
		if processedNames[probeConfig.Name] {
			s.moduleLogger.Debug().
				Str("probe_name", probeConfig.Name).
				Msg("Skipping duplicate probe name")
			continue
		}
		processedNames[probeConfig.Name] = true

		// A disabled probe is deliberately absent from validProbeIds, so phase
		// 2 stops it if it was running: flipping enabled to false in a config
		// reload must take effect without restarting the agent.
		//
		// Logged at Info, not Debug: "this probe collects nothing" is exactly
		// the question an operator asks the log, and answering it only under a
		// debug build is how a deliberate silence becomes indistinguishable
		// from a broken one.
		if !probeConfig.IsEnabled() {
			s.moduleLogger.Info().
				Str("probe_name", probeConfig.Name).
				Str("probe_type", probeConfig.Type).
				Msg("Probe disabled by configuration (enabled: false) - not started")
			continue
		}

		probeId := probes.GenerateProbeId(probeConfig)
		validProbeIds = append(validProbeIds, probeId)

		probeExists := false
		for _, startedProbe := range s.startedProbes {
			if startedProbe.ProbeId == probeId {
				probeExists = true
				startedProbe.ApplyGovernance(probeConfig)
				break
			}
		}
		if probeExists {
			s.moduleLogger.Debug().
				Str("probe_id", probeId).
				Str("probe_name", probeConfig.Name).
				Msg("Probe already running, skipping")
			continue
		}
		toStart = append(toStart, pendingStart{id: probeId, config: probeConfig})
	}

	// Phase 2: Stop removed probes
	activeProbes := []*probes.ProbePoller{}
	stoppedCount := 0

	for _, startedProbe := range s.startedProbes {
		found := false
		for _, validProbeId := range validProbeIds {
			if startedProbe.ProbeId == validProbeId {
				found = true
				break
			}
		}
		if found {
			// Keep active probes
			activeProbes = append(activeProbes, startedProbe)
		} else {
			// Shutdown and remove probe
			s.moduleLogger.Info().
				Str("probe_id", startedProbe.ProbeId).
				Str("probe_name", startedProbe.Probe.GetName()).
				Msg("Stopping removed probe")

			err := stopProbe(startedProbe)
			if err != nil {
				s.moduleLogger.Error().
					Str("probe_id", startedProbe.ProbeId).
					Str("probe_name", startedProbe.Probe.GetName()).
					Err(err).
					Msg("Error stopping probe")
			} else {
				s.moduleLogger.Info().
					Str("probe_id", startedProbe.ProbeId).
					Str("probe_name", startedProbe.Probe.GetName()).
					Msg("Probe stopped successfully")
				stoppedCount++
			}
		}
	}
	s.startedProbes = activeProbes

	// Phase 3: Start new probes, now that what they replace is gone.
	// Listener probes go first so the semaphore never queues a receiver
	// behind slow probes; the starts themselves run in parallel.
	sort.SliceStable(toStart, func(i, j int) bool {
		return isListenerProbeType(toStart[i].config.Type) && !isListenerProbeType(toStart[j].config.Type)
	})

	type startResult struct {
		poller  *probes.ProbePoller
		err     error
		elapsed time.Duration
	}
	results := make([]startResult, len(toStart))
	sem := make(chan struct{}, maxParallelProbeStarts)
	var wg sync.WaitGroup
	for i, p := range toStart {
		// The edition and licence checks need no I/O and stay sequential;
		// a probe they refuse never reaches a goroutine.
		if err := s.checkStartAllowed(p.config); err != nil {
			results[i] = startResult{err: err}
			continue
		}
		s.moduleLogger.Info().
			Str("probe_id", p.id).
			Str("probe_name", p.config.Name).
			Any("probe_params", configuration.SanitizeParamsForLog(p.config.Params)).
			Msg("Starting new probe")

		wg.Add(1)
		go func(i int, p pendingStart) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			timer := time.AfterFunc(slowStartNoticeDelay, func() {
				s.moduleLogger.Warn().
					Str("probe_name", p.config.Name).
					Str("probe_type", p.config.Type).
					Msg("Probe start is slow; the other probes are not waiting for it")
			})
			poller, err := s.buildAndStart(p.config)
			timer.Stop()
			results[i] = startResult{poller: poller, err: err, elapsed: time.Since(start)}
		}(i, p)
	}
	wg.Wait()

	// Bookkeeping stays sequential in toStart order so logs, retries and
	// counters behave exactly as they did when starts were serial.
	startedCount := 0
	for i, p := range toStart {
		r := results[i]
		probeLogger := s.getLoggerForProbe(p.config)
		if r.err != nil {
			noRetry := errors.Is(r.err, types.ErrCredentialsRejected)
			if noRetry {
				probeLogger.Error().Err(r.err).
					Str("probe_name", p.config.Name).
					Str("probe_type", p.config.Type).
					Int64("start_ms", r.elapsed.Milliseconds()).
					Msg("Error starting probe: the target refused the credentials; not retried automatically, to avoid locking the account. Fix them, then reload the configuration or restart the agent")
			} else {
				probeLogger.Error().Err(r.err).
					Str("probe_name", p.config.Name).
					Str("probe_type", p.config.Type).
					Dur("retry_in", probeStartRetryInterval).
					Int64("start_ms", r.elapsed.Milliseconds()).
					Msg("Error starting probe")
			}
			s.failedProbes[p.id] = failedProbe{config: p.config, reason: r.err.Error(), noRetry: noRetry}
			if !noRetry {
				select {
				case s.retryWake <- struct{}{}:
				default:
				}
			}
		} else {
			delete(s.failedProbes, p.id)
			startedCount++
			s.startedProbes = append(s.startedProbes, r.poller)
			s.moduleLogger.Info().
				Str("probe_id", p.id).
				Str("probe_name", p.config.Name).
				Int64("start_ms", r.elapsed.Milliseconds()).
				Msg("Probe started successfully")
		}
	}
	activeProbes = s.startedProbes

	// Update the slice to contain only active probes
	s.startedProbes = activeProbes

	// A probe removed from the configuration is no longer owed a retry.
	valid := make(map[string]bool, len(validProbeIds))
	for _, id := range validProbeIds {
		valid[id] = true
	}
	for id := range s.failedProbes {
		if !valid[id] {
			delete(s.failedProbes, id)
		}
	}

	// Publish the live probe set for the Prometheus bridge to read
	// (senhub_agent_probe_count / senhub_agent_probes_active).
	publishActiveProbes(s.startedProbes)
	s.publishFailedProbes()

	s.moduleLogger.Info().
		Int("probes_started", startedCount).
		Int("probes_stopped", stoppedCount).
		Int("probes_active", len(activeProbes)).
		Msg("Configuration synchronization completed")

	return nil
}

// isListenerProbeType reports whether a probe type opens a listener. Those
// are sorted ahead of the others in Phase 3 so a slow probe cannot hold the
// semaphore slot a receiver needs.
func isListenerProbeType(probeType string) bool {
	switch probeType {
	case "otlp_receiver", "syslog", "snmp_trap":
		return true
	}
	return false
}

// StopBudget gives the probe pool longer than the default: probes close
// remote connections (DB handles, SSH sessions, IPMI, listeners) and a
// pool of a few dozen of them serialises those closes.
func (s *sensor) StopBudget() time.Duration { return 8 * time.Second }

func (s *sensor) Start(ctx context.Context) error {
	s.moduleLogger.Info().Msg("Starting sensor")

	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()

	if err := s.SyncConfiguration(); err != nil {
		return fmt.Errorf("failed to sync configuration: %w", err)
	}

	s.moduleLogger.Info().Msg("Starting sensor service")
	go s.retryFailedProbes(ctx)
	s.configProvider.OnConfigChanged(func(string) {
		if err := s.SyncConfiguration(); err != nil {
			s.moduleLogger.Error().Err(err).Msg("Failed to sync configuration on config change")
		}
	})
	return nil
}

func (s *sensor) getLoggerForProbe(probeConfig configuration.ProbeConfig) *logger.Logger {
	// Return the base logger for probe constructor compatibility
	// Probes will create their own ModuleLogger from this base logger
	return s.moduleLogger.Logger
}

// startProbe is the sequential path used by the retry timer: the no-I/O
// checks, then the build-and-start, then the startedProbes append.
func (s *sensor) startProbe(probeConfig configuration.ProbeConfig) error {
	probeId := probes.GenerateProbeId(probeConfig)

	for _, startedProbe := range s.startedProbes {
		if startedProbe.ProbeId == probeId {
			return nil
		}
	}

	if err := s.checkStartAllowed(probeConfig); err != nil {
		return err
	}

	probePoller, err := s.buildAndStart(probeConfig)
	if err != nil {
		return err
	}
	s.startedProbes = append(s.startedProbes, probePoller)
	return nil
}

// checkStartAllowed runs the checks that need no I/O: the probe type must
// exist in this build and the licence must allow it.
func (s *sensor) checkStartAllowed(probeConfig configuration.ProbeConfig) error {
	probeType := probeConfig.Type
	if probeType == "" {
		// Fallback to name if type is not set (for backward compatibility)
		probeType = probeConfig.Name
	}

	// A type this binary does not carry is not a licensing question.
	if _, registered := probes.LookupProbeConstructor(probeType); !registered {
		return errors.New(license.NotInThisBuild(probeType))
	}

	// If licenseValidator is nil (safe mode), only allow free tier probes
	if s.licenseValidator == nil {
		freeTierProbes := license.GetFreeTierProbes()
		isFreeTier := false
		for _, freeProbe := range freeTierProbes {
			if freeProbe == probeType {
				isFreeTier = true
				break
			}
		}
		if !isFreeTier {
			s.moduleLogger.Warn().
				Str("probe_type", probeType).
				Str("probe_name", probeConfig.Name).
				Strs("free_tier_probes", freeTierProbes).
				Msg("License validator unavailable - only free tier probes allowed")
			return fmt.Errorf("probe %q requires a valid license validator", probeType)
		}
		// Free tier probe - allow it to continue startup
	} else {
		// Normal license validation with validator
		if !s.licenseValidator.IsProbeAuthorized(s.license, probeType) {
			// Get list of free tier probes for helpful error message
			freeTierProbes := license.GetFreeTierProbes()
			s.moduleLogger.Warn().
				Str("probe_type", probeType).
				Str("probe_name", probeConfig.Name).
				Strs("free_tier_probes", freeTierProbes).
				Msg("Probe not authorized by license - skipping (upgrade license to enable)")
			return fmt.Errorf("probe %q requires a valid license", probeType)
		}
	}
	return nil
}

// buildAndStart constructs and starts one probe. It is safe to call from
// Phase 3 goroutines while the caller holds s.mu: it only reads s.runCtx,
// s.addDataPoint and the logger, and never mutates sensor state.
func (s *sensor) buildAndStart(probeConfig configuration.ProbeConfig) (*probes.ProbePoller, error) {
	probePoller, err := probes.NewProbePoller(
		probeConfig,
		s.getLoggerForProbe(probeConfig),
		s.addDataPoint,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to create probe poller: %w", err)
	}

	runCtx := s.runCtx
	if runCtx == nil {
		// Only reachable when a caller drives SyncConfiguration without
		// Start (tests, the config-show path). Shutdown still stops the
		// probe; it simply has no external cancellation.
		runCtx = context.Background()
	}

	if err := probePoller.Start(runCtx); err != nil {
		// Not kept as started: a poller left in startedProbes after a
		// failed start reads as running to every later sync, which then
		// never tries it again.
		if stopErr := stopProbe(probePoller); stopErr != nil {
			s.moduleLogger.Warn().Err(stopErr).Str("probe_name", probeConfig.Name).Msg("Stopping a probe that failed to start")
		}
		return nil, err
	}
	return probePoller, nil
}

// probeStopBudget bounds the stop of one probe on a reload. The sensor
// lock is held meanwhile, and a probe blocked in a collection against an
// unreachable target would otherwise hold it, and every later reload,
// for as long as that collection lasts.
var probeStopBudget = 5 * time.Second

func stopProbe(p *probes.ProbePoller) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeStopBudget)
	defer cancel()
	return p.Shutdown(ctx)
}

// publishFailedProbes hands the probes that failed to start to agentstate,
// where they count in the total, never as healthy, and carry their reason
// to the console. Callers hold s.mu.
func (s *sensor) publishFailedProbes() {
	out := make(map[string]string, len(s.failedProbes))
	for id, f := range s.failedProbes {
		out[id] = f.reason
	}
	agentstate.SetStartFailedProbes(out)
}

// retryFailedProbes tries the probes that failed to start again, until
// ctx ends. It sleeps on a channel while no probe is waiting for a retry
// and runs a timer only while one is.
func (s *sensor) retryFailedProbes(ctx context.Context) {
	for {
		if !s.hasRetryableFailedProbes() {
			select {
			case <-ctx.Done():
				return
			case <-s.retryWake:
				continue
			}
		}
		timer := time.NewTimer(probeStartRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.retryFailedProbesOnce()
		}
	}
}

func (s *sensor) hasRetryableFailedProbes() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.failedProbes {
		if !f.noRetry {
			return true
		}
	}
	return false
}

// retryFailedProbesOnce is one retry round. A retry failing the same way
// is logged at debug, a new reason is a warning, a recovery is announced.
func (s *sensor) retryFailedProbesOnce() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failedProbes) == 0 {
		return
	}
	for id, f := range s.failedProbes {
		if f.noRetry {
			continue
		}
		err := s.startProbe(f.config)
		switch {
		case err == nil:
			delete(s.failedProbes, id)
			s.moduleLogger.Info().
				Str("probe_name", f.config.Name).
				Str("probe_type", f.config.Type).
				Msg("Probe started after a failed start")
		case err.Error() != f.reason:
			s.failedProbes[id] = failedProbe{config: f.config, reason: err.Error(), noRetry: errors.Is(err, types.ErrCredentialsRejected)}
			s.moduleLogger.Warn().Err(err).
				Str("probe_name", f.config.Name).
				Str("probe_type", f.config.Type).
				Msg("Probe still cannot start, for a different reason")
		default:
			s.moduleLogger.Debug().Err(err).
				Str("probe_name", f.config.Name).
				Msg("Probe still cannot start")
		}
	}
	publishActiveProbes(s.startedProbes)
	s.publishFailedProbes()
}

func (s *sensor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.moduleLogger.Info().Msg("Shutting down sensor")

	for _, probePoller := range s.startedProbes {
		s.moduleLogger.Debug().
			Str("probe_name", probePoller.GetName()).
			Msg("Shutting down sensor")

		err := probePoller.Shutdown(ctx)
		if err != nil {
			s.moduleLogger.Error().
				Err(err).
				Str("probe_name", probePoller.GetName()).
				Msg("Error shutting down probe")
		}
	}

	// Drop the pool so a second Shutdown is a no-op and a restart does
	// not re-stop pollers that are already gone.
	s.startedProbes = nil
	s.runCtx = nil
	publishActiveProbes(nil)
	return nil
}

// publishActiveProbes publishes the IDs of currently-running probes to the
// agentstate package so the Prometheus bridge can expose
// senhub_agent_probe_count and senhub_agent_probes_healthy without a
// direct dependency from the http strategy back to the sensor.
//
// We send IDs (not the probe objects) because health is pushed via
// RecordProbeHealth from ProbePoller.collect() — the bridge never needs
// to call back into a probe instance, eliminating any chance of a scrape
// triggering a Collect() (and the resulting wasted CPU + races we used
// to have when IsHealthy() implementations re-collected on demand).
func publishActiveProbes(pollers []*probes.ProbePoller) {
	out := make([]string, 0, len(pollers))
	byName := make(map[string]string, len(pollers))
	intervals := make(map[string]int, len(pollers))
	for _, pp := range pollers {
		if pp != nil {
			out = append(out, pp.ProbeId)
			byName[pp.Probe.GetName()] = pp.ProbeId
			intervals[pp.Probe.GetName()] = int(pp.Probe.GetInterval().Seconds())
		}
	}
	agentstate.SetActiveProbes(out)
	agentstate.SetActiveProbeNames(byName)
	agentstate.SetActiveProbeIntervals(intervals)
}
