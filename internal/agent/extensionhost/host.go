// Package extensionhost builds the registered extensions for an agent and
// gives each the Host it reads the agent through.
package extensionhost

import (
	"context"
	"os"
	"slices"
	"time"

	"senhub-agent.go/internal/agent/lifecycle"
	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/exporthook"
	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/instanceid"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/agent/services/logger"
)

// ConfigSource is the part of the local configuration a Host reads.
type ConfigSource interface {
	ExtensionBlock(name string) (map[string]interface{}, bool)
	OnConfigChanged(callback func(string))
	GetAuthenticationKey() string
	GetConfigPath() string
	GetConfiguration() configuration.ConfigurationData
	Reload() error
}

// Deps is what the agent hands the extensions.
type Deps struct {
	Logger    *logger.Logger
	Config    ConfigSource
	Version   string
	StartTime time.Time
	// IsProbeRegistered tells the edition: a build that carries a paid
	// probe is the enterprise edition.
	IsProbeRegistered func(probeType string) bool
}

// Services builds every valid registered extension. An extension whose
// constructor fails is left out with an error line: it must not keep the
// agent from running.
func Services(d Deps) []lifecycle.Service {
	log := logger.NewModuleLogger(d.Logger, "extension")
	for _, p := range extension.Problems() {
		log.Warn().Str("problem", p).Msg("Extension registration ignored")
	}

	var out []lifecycle.Service
	for _, e := range extension.Registered() {
		h := &host{deps: d, name: e.Name, log: log}
		svc, err := e.New(h)
		if err != nil {
			log.Error().Err(err).Str("extension", e.Name).Msg("Extension not started: its constructor failed")
			continue
		}
		if svc == nil {
			log.Error().Str("extension", e.Name).Msg("Extension not started: its constructor returned no service")
			continue
		}
		out = append(out, &service{name: e.Name, svc: svc, budget: e.StopBudget})
	}
	return out
}

type service struct {
	name   string
	svc    extension.Service
	budget time.Duration
}

func (s *service) GetName() string { return "Extension " + s.name }

func (s *service) Start(ctx context.Context) error { return s.svc.Start(ctx) }

func (s *service) Shutdown(ctx context.Context) error {
	defer extension.SetStatus(s.name, nil)
	return s.svc.Shutdown(ctx)
}

func (s *service) StopBudget() time.Duration {
	if s.budget > 0 {
		return s.budget
	}
	return lifecycle.DefaultStopBudget
}

type host struct {
	deps Deps
	name string
	log  *logger.ModuleLogger
}

func (h *host) Logger() *logger.Logger { return h.deps.Logger }

func (h *host) ConfigBlock(name string) (map[string]any, bool) {
	if h.deps.Config == nil {
		return nil, false
	}
	return h.deps.Config.ExtensionBlock(name)
}

func (h *host) OnConfigChange(fn func()) {
	if h.deps.Config == nil || fn == nil {
		return
	}
	h.deps.Config.OnConfigChanged(func(string) { fn() })
}

func (h *host) Identity() extension.Identity {
	id := extension.Identity{
		Version: h.deps.Version,
		Edition: Edition(h.deps.IsProbeRegistered),
	}
	if h.deps.Config == nil {
		return id
	}
	id.InstanceID = configuration.AgentInstanceID(h.deps.Config.GetAuthenticationKey())
	d := h.deps.Config.GetConfiguration()
	tier, lic := LicenseInForce(d.Agent.License, d.Agent.AuthenticationKey, func() (license.Validator, error) {
		return license.GetDefaultValidator(7)
	})
	id.LicenseTier = tier
	if lic != nil {
		id.LicenseID = lic.ID
		id.LicenseExpiresAt = lic.ExpiresAt
	}
	return id
}

func (h *host) HostAttributes() (map[string]string, error) {
	return common.GetHostResourceAttributes()
}

func (h *host) Authorized(feature string) bool { return exporthook.Authorized(feature) }

func (h *host) Health() extension.Health {
	if h.deps.Config == nil {
		return extension.Health{Healthy: true, StartTime: h.deps.StartTime}
	}
	return CollectHealth(h.deps.Config.GetConfiguration(), h.deps.StartTime)
}

func (h *host) EffectiveConfig() ([]extension.ConfigFile, error) {
	if h.deps.Config == nil {
		return nil, nil
	}
	files, err := configuration.RedactedConfigFiles(h.deps.Config.GetConfigPath(), h.log)
	if err != nil {
		return nil, err
	}
	out := make([]extension.ConfigFile, len(files))
	for i, f := range files {
		out[i] = extension.ConfigFile{Name: f.Name, Body: f.Body}
	}
	return out, nil
}

func (h *host) PublishStatus(status map[string]any) { extension.SetStatus(h.name, status) }

func (h *host) ReloadConfig() error {
	if h.deps.Config == nil {
		return nil
	}
	return h.deps.Config.Reload()
}

func (h *host) StateDir() string {
	dir := instanceid.OwnStateDir()
	if dir == "" {
		return ""
	}
	// 0700 on Unix; Windows ignores the mode and inherits the data
	// folder ACL, so the same call is right there too.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		if h.log != nil {
			h.log.Warn().Err(err).Str("path", dir).Msg("Extension state directory could not be created")
		}
		return ""
	}
	return dir
}

func (h *host) SetLogLevel(level string) error {
	if level == "" {
		logger.ClearOverallLevel()
		return nil
	}
	return logger.SetOverallLevel(level)
}

// CollectHealth reads the probe run state, the start failures, the output
// failures and the export activity: what status and doctor read.
func CollectHealth(cfg configuration.ConfigurationData, start time.Time) extension.Health {
	health := extension.Health{Healthy: true, StartTime: start}

	for _, p := range cfg.Probes {
		rs := agentstate.GetProbeRunState(p.ID())
		ph := extension.ProbeHealth{
			Name:      p.Name,
			Enabled:   p.IsEnabled(),
			Running:   rs.Running,
			Health:    rs.Health,
			LastError: rs.LastError,
			StartedAt: rs.StartedAt,
			LastCycle: rs.LastCycle,
		}
		if ph.Enabled && ph.Health == "failed" {
			health.Healthy = false
		}
		health.Probes = append(health.Probes, ph)
	}

	failures := agentstate.GetStrategyFailures()
	failing := agentstate.FailingExports()
	for _, sc := range cfg.StorageConfig {
		oh := extension.OutputHealth{Name: sc.Name}
		if f, ok := failures[sc.Name]; ok {
			oh.FailureReason, oh.FailureDetail = f.Reason, f.Detail
			health.Healthy = false
		}
		if slices.Contains(failing, sc.Name) {
			oh.Failing = true
			health.Healthy = false
		}
		oh.LastError = latestExportError(agentstate.GetExportActivities(sc.Name))
		health.Outputs = append(health.Outputs, oh)
	}
	return health
}

func latestExportError(activities map[string]agentstate.ExportActivity) string {
	var latest time.Time
	var lastError string
	for _, activity := range activities {
		if activity.LastFailure.After(latest) {
			latest = activity.LastFailure
			lastError = activity.LastError
		}
	}
	return lastError
}

// Edition names the build: "enterprise" when it carries a probe of the
// paid catalogue, "oss" otherwise.
func Edition(isRegistered func(probeType string) bool) string {
	if isRegistered == nil {
		return ""
	}
	for _, probeType := range license.KnownPaidProbes() {
		if isRegistered(probeType) {
			return "enterprise"
		}
	}
	return "oss"
}

// LicenseTier is the tier the licence token grants this agent: "free"
// without a token, or with one that is invalid, bound to another agent
// or expired past its grace period; "" when no validator is available.
func LicenseTier(token, agentKey string, newValidator func() (license.Validator, error)) string {
	tier, _ := LicenseInForce(token, agentKey, newValidator)
	return tier
}

// LicenseInForce is LicenseTier with the licence that grants it, nil
// when the tier is "free" or unknown.
func LicenseInForce(token, agentKey string, newValidator func() (license.Validator, error)) (string, *license.License) {
	if token == "" {
		return string(license.TierFree), nil
	}
	if newValidator == nil {
		return "", nil
	}
	validator, err := newValidator()
	if err != nil {
		return "", nil
	}
	lic, err := validator.ValidateLicense(token)
	if err != nil || lic == nil {
		return string(license.TierFree), nil
	}
	if agentKey != "" && !license.VerifyBinding(token, agentKey, lic) {
		return string(license.TierFree), nil
	}
	if lic.IsExpired && !validator.IsInGracePeriod(lic) {
		return string(license.TierFree), nil
	}
	return string(lic.Tier), lic
}
