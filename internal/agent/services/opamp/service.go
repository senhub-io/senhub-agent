package opamp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
	"google.golang.org/protobuf/proto"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
	agentlogger "senhub-agent.go/internal/agent/services/logger"
)

var bearerRedactRe = regexp.MustCompile(`(?i)Bearer\s+\S+`)

type ConfigSource interface {
	GetOpAMPConfig() *configuration.OpAMPConfig
	GetConfigPath() string
	GetAuthenticationKey() string
	GetConfiguration() configuration.ConfigurationData
	OnConfigChanged(callback func(string))
}

type Options struct {
	Logger    *agentlogger.Logger
	Config    ConfigSource
	Version   string
	StartTime time.Time
	Edition   string

	HostAttributes func() (map[string]string, error)
	LicenseTier    func() string
	EffectiveFiles func(configPath string) ([]configuration.RedactedFile, error)
	HealthInput    func() healthInput
	HealthInterval time.Duration
}

type Service struct {
	opts Options
	log  *agentlogger.ModuleLogger

	mu                 sync.Mutex
	ctx                context.Context
	callbackRegistered bool
	closed             bool
	sess               *session

	remoteConfigLogged atomic.Bool
}

type connState int

const (
	connNever connState = iota
	connConnected
	connDisconnected
)

type session struct {
	cfg    configuration.OpAMPConfig
	token  string
	redact func(string) string
	log    *agentlogger.ModuleLogger

	client         client.OpAMPClient
	heartbeat      time.Duration
	healthInterval time.Duration
	healthInput    func() healthInput

	healthStop    chan struct{}
	healthDone    chan struct{}
	healthOnce    sync.Once
	healthStarted bool

	connMu             sync.Mutex
	connState          connState
	firstFailureLogged bool

	descMu      sync.Mutex
	currentDesc *protobufs.AgentDescription

	effMu     sync.Mutex
	effective *protobufs.EffectiveConfig

	healthMu        sync.Mutex
	lastFingerprint string
	lastHealthPush  time.Time
}

func New(opts Options) *Service {
	if opts.HealthInterval <= 0 {
		opts.HealthInterval = 5 * time.Second
	}

	s := &Service{opts: opts}
	s.log = agentlogger.NewModuleLogger(opts.Logger, "opamp")

	if s.opts.HostAttributes == nil {
		s.opts.HostAttributes = common.GetHostResourceAttributes
	}
	if s.opts.EffectiveFiles == nil {
		s.opts.EffectiveFiles = func(configPath string) ([]configuration.RedactedFile, error) {
			return configuration.RedactedConfigFiles(configPath, s.log)
		}
	}
	if s.opts.HealthInput == nil {
		s.opts.HealthInput = func() healthInput {
			if s.opts.Config == nil {
				return healthInput{StartTime: opts.StartTime, Now: time.Now()}
			}
			return collectHealthInput(s.opts.Config.GetConfiguration(), opts.StartTime, time.Now())
		}
	}
	if s.opts.LicenseTier == nil {
		s.opts.LicenseTier = func() string {
			if s.opts.Config == nil {
				return ""
			}
			d := s.opts.Config.GetConfiguration()
			return licenseTier(d.Agent.License, d.Agent.AuthenticationKey, func() (license.Validator, error) {
				return license.GetDefaultValidator(7)
			})
		}
	}

	return s
}

func (s *Service) GetName() string           { return "OpAMP" }
func (s *Service) StopBudget() time.Duration { return 5 * time.Second }

func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.ctx == nil {
		s.ctx = ctx
	}
	if s.opts.Config != nil && !s.callbackRegistered {
		s.opts.Config.OnConfigChanged(func(string) { s.reload() })
		s.callbackRegistered = true
	}
	s.mu.Unlock()

	s.reload()
	return nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Held across the stop so a configuration change arriving during
	// shutdown cannot start a client after it; no client callback takes
	// this lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopCurrentSessionLocked(ctx)
	agentstate.SetOpAMPConnected(false)
	return nil
}

func (s *Service) Connected() bool {
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		return false
	}
	return sess.connected()
}

func (s *Service) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	s.reloadLocked()
}

func (s *Service) reloadLocked() {
	if s.opts.Config == nil {
		s.stopCurrentSessionWithTimeoutLocked()
		agentstate.SetOpAMPEnabled(false)
		return
	}

	cfg := s.opts.Config.GetOpAMPConfig()
	if cfg == nil || !cfg.Enabled {
		if s.sess != nil {
			s.log.Info().Msg("OpAMP client stopped: disabled in the configuration")
		}
		s.stopCurrentSessionWithTimeoutLocked()
		agentstate.SetOpAMPEnabled(false)
		return
	}

	errs := configuration.ValidateOpAMP(cfg)
	if len(errs) > 0 {
		s.stopCurrentSessionWithTimeoutLocked()
		agentstate.SetOpAMPEnabled(false)

		problems := make([]string, len(errs))
		for i, err := range errs {
			problems[i] = err.Error()
		}
		s.log.Warn().Strs("problems", problems).
			Msg("OpAMP is enabled but its configuration is invalid; the client is not started")
		return
	}

	n := cfg.Normalized()
	if s.sess != nil && s.sess.sameConnectionSettings(n) {
		s.refreshSessionLocked(s.sess)
		return
	}

	s.stopCurrentSessionWithTimeoutLocked()
	agentstate.SetOpAMPEnabled(true)
	s.startSessionLocked(n)
}

func (s *Service) stopCurrentSessionWithTimeoutLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.stopCurrentSessionLocked(ctx)
}

func (s *Service) stopCurrentSessionLocked(ctx context.Context) {
	if s.sess == nil {
		return
	}
	sess := s.sess
	s.sess = nil
	sess.stop(ctx)
	agentstate.SetOpAMPConnected(false)
}

func (s *Service) startSessionLocked(n configuration.OpAMPConfig) {
	if s.ctx == nil {
		s.ctx = context.Background()
	}

	instanceID := configuration.AgentInstanceID(s.opts.Config.GetAuthenticationKey())
	uid, err := instanceUID(instanceID)
	if err != nil {
		s.log.Warn().Msg("OpAMP client not started: the agent has no instance id yet")
		return
	}

	redact := makeRedact(n.Token)

	hb, err := n.Heartbeat()
	if err != nil {
		s.log.Warn().Str("error", redact(err.Error())).Msg("OpAMP client not started")
		return
	}

	effective := s.recomputeEffective(n.Token)
	desc := s.buildDescription(n.Token, true)
	initialHealth := buildHealth(s.opts.HealthInput(), redact)

	adapter := opampLogger{log: s.log, redact: redact, remoteConfigIgnored: s.noteRemoteConfigIgnored}

	var c client.OpAMPClient
	if n.Transport == configuration.OpAMPTransportHTTP {
		c = client.NewHTTP(adapter)
	} else {
		c = client.NewWebSocket(adapter)
	}

	capabilities := protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus |
		protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig |
		protobufs.AgentCapabilities_AgentCapabilities_ReportsHealth |
		protobufs.AgentCapabilities_AgentCapabilities_ReportsHeartbeat
	if err := c.SetCapabilities(&capabilities); err != nil {
		s.log.Warn().Str("error", redact(err.Error())).Msg("OpAMP client not started")
		return
	}

	if err := c.SetAgentDescription(desc); err != nil {
		s.log.Warn().Str("error", redact(err.Error())).Msg("OpAMP client not started")
		return
	}

	if err := c.SetHealth(initialHealth); err != nil {
		s.log.Warn().Str("error", redact(err.Error())).Msg("OpAMP client not started")
		return
	}

	sess := &session{
		cfg:            n,
		token:          n.Token,
		redact:         redact,
		log:            s.log,
		heartbeat:      hb,
		healthInterval: s.opts.HealthInterval,
		healthInput:    s.opts.HealthInput,
		effective:      effective,
		currentDesc:    desc,
		healthStop:     make(chan struct{}),
		healthDone:     make(chan struct{}),
	}
	sess.healthMu.Lock()
	sess.lastFingerprint = healthFingerprint(initialHealth)
	sess.lastHealthPush = time.Now()
	sess.healthMu.Unlock()

	settings, err := s.buildStartSettings(n, uid, hb, redact, sess)
	if err != nil {
		agentstate.IncrementOpAMPErrors(agentstate.OpAMPErrorConnect)
		s.log.Warn().Str("error", redact(err.Error())).Msg("OpAMP client not started")
		return
	}

	if err := c.Start(s.ctx, settings); err != nil {
		agentstate.IncrementOpAMPErrors(agentstate.OpAMPErrorConnect)
		s.log.Warn().Str("error", redact(err.Error())).Msg("OpAMP client not started")
		return
	}

	sess.client = c
	sess.healthStarted = true
	s.sess = sess
	go sess.healthLoop(s.ctx)
}

func (s *Service) buildStartSettings(
	n configuration.OpAMPConfig,
	uid types.InstanceUid,
	hb time.Duration,
	redact func(string) string,
	sess *session,
) (types.StartSettings, error) {
	tlsConfig, err := s.buildTLSConfig(n)
	if err != nil {
		return types.StartSettings{}, err
	}

	callbacks := types.Callbacks{
		OnConnect: func(ctx context.Context) {
			sess.onConnect(ctx)
		},
		OnConnectFailed: func(ctx context.Context, err error) {
			sess.onConnectFailed(ctx, err)
		},
		OnError: func(ctx context.Context, resp *protobufs.ServerErrorResponse) {
			agentstate.IncrementOpAMPErrors(agentstate.OpAMPErrorServer)
			if resp != nil {
				sess.log.Debug().Str("error", sess.redact(resp.GetErrorMessage())).
					Msg("OpAMP server returned an error")
			} else {
				sess.log.Debug().Msg("OpAMP server returned an error")
			}
		},
		GetEffectiveConfig: func(ctx context.Context) (*protobufs.EffectiveConfig, error) {
			return sess.getEffective(), nil
		},
	}
	callbacks.SetDefaults()

	header := make(http.Header)
	header.Set("Authorization", "Bearer "+n.Token)

	heartbeat := hb
	return types.StartSettings{
		OpAMPServerURL:    n.Endpoint,
		Header:            header,
		InstanceUid:       uid,
		HeartbeatInterval: &heartbeat,
		TLSConfig:         tlsConfig,
		Callbacks:         callbacks,
		BackoffPolicy: func() types.BackoffPolicy {
			b := backoff.NewExponentialBackOff()
			b.MaxInterval = 5 * time.Minute
			b.MaxElapsedTime = 0
			return b
		},
	}, nil
}

func (s *Service) buildTLSConfig(n configuration.OpAMPConfig) (*tls.Config, error) {
	u, err := url.Parse(n.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("opamp.endpoint: %w", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "wss", "https":
	default:
		return nil, nil
	}

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: n.TLS.InsecureSkipVerify, // #nosec G402 - operator-controlled, off by default
	}

	if n.TLS.CAFile != "" {
		pemData, err := os.ReadFile(n.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("opamp.tls.ca_file %q: %w", n.TLS.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("opamp.tls.ca_file %q: no certificate parsed", n.TLS.CAFile)
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}

func (s *Service) buildDescription(token string, logHostErr bool) *protobufs.AgentDescription {
	input := descriptionInput{
		InstanceID:  configuration.AgentInstanceID(s.opts.Config.GetAuthenticationKey()),
		Version:     s.opts.Version,
		Edition:     s.opts.Edition,
		LicenseTier: s.opts.LicenseTier(),
	}

	if s.opts.HostAttributes != nil {
		attrs, err := s.opts.HostAttributes()
		if err == nil {
			input.Host = attrs
		} else if logHostErr {
			s.log.Warn().
				Str("error", makeRedact(token)(err.Error())).
				Msg("OpAMP host attributes unavailable; reporting without them")
		}
	}

	return buildDescription(input)
}

func (s *Service) recomputeEffective(token string) *protobufs.EffectiveConfig {
	files, err := s.opts.EffectiveFiles(s.opts.Config.GetConfigPath())
	if err != nil {
		agentstate.IncrementOpAMPErrors(agentstate.OpAMPErrorEffectiveConfig)
		s.log.Warn().
			Str("error", makeRedact(token)(err.Error())).
			Msg("OpAMP effective configuration could not be computed")
		return nil
	}

	ec := buildEffectiveConfig(files)
	if size := effectiveConfigSize(ec); size > effectiveConfigNotice {
		s.log.Info().
			Int("bytes", size).
			Msg("effective configuration exceeds 1 MiB; it is sent whole and the server may keep only part of it")
	}
	return ec
}

func (s *Service) refreshSessionLocked(sess *session) {
	ec := s.recomputeEffective(sess.cfg.Token)
	if ec != nil {
		sess.setEffective(ec)
		ctx := s.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if err := sess.client.UpdateEffectiveConfig(ctx); err != nil {
			sess.reportSetError("OpAMP effective configuration update failed", err)
		} else {
			agentstate.IncrementOpAMPMessagesSent()
		}
	}

	desc := s.buildDescription(sess.cfg.Token, false)
	sess.updateDescription(desc)
	sess.pushHealth()
}

func (s *Service) noteRemoteConfigIgnored() {
	if s.remoteConfigLogged.CompareAndSwap(false, true) {
		s.log.Info().Msg("OpAMP server offered a remote configuration; this agent does not accept remote configuration and ignored it")
	}
}

func makeRedact(token string) func(string) string {
	return func(s string) string {
		if token != "" {
			s = strings.ReplaceAll(s, token, "***")
		}
		s = bearerRedactRe.ReplaceAllString(s, "Bearer ***")
		return configuration.RedactSecretsInString(s)
	}
}

// remoteConfigIgnoredLog is how opamp-go says it dropped an offered remote
// configuration. Without AcceptsRemoteConfig the offer never reaches
// OnMessage, so this line is the only place the agent can see it.
const remoteConfigIgnoredLog = "Ignoring RemoteConfig"

type opampLogger struct {
	log                 *agentlogger.ModuleLogger
	redact              func(string) string
	remoteConfigIgnored func()
}

func (l opampLogger) Debugf(ctx context.Context, format string, v ...interface{}) {
	if l.remoteConfigIgnored != nil && strings.HasPrefix(format, remoteConfigIgnoredLog) {
		l.remoteConfigIgnored()
	}
	l.log.Debug().Str("detail", l.redact(fmt.Sprintf(format, v...))).Msg("opamp-go")
}

func (l opampLogger) Errorf(ctx context.Context, format string, v ...interface{}) {
	l.log.Debug().Str("detail", l.redact(fmt.Sprintf(format, v...))).Msg("opamp-go")
}

func (sess *session) sameConnectionSettings(n configuration.OpAMPConfig) bool {
	return sess != nil && sess.cfg == n
}

func (sess *session) getEffective() *protobufs.EffectiveConfig {
	sess.effMu.Lock()
	defer sess.effMu.Unlock()
	return sess.effective
}

func (sess *session) setEffective(ec *protobufs.EffectiveConfig) {
	sess.effMu.Lock()
	defer sess.effMu.Unlock()
	sess.effective = ec
}

func (sess *session) updateDescription(desc *protobufs.AgentDescription) {
	if desc == nil {
		return
	}

	sess.descMu.Lock()
	changed := sess.currentDesc == nil || !proto.Equal(sess.currentDesc, desc)
	if changed {
		sess.currentDesc = desc
	}
	sess.descMu.Unlock()

	if !changed {
		return
	}

	if err := sess.client.SetAgentDescription(desc); err != nil {
		sess.reportSetError("OpAMP agent description update failed", err)
		return
	}
	agentstate.IncrementOpAMPMessagesSent()
}

func (sess *session) pushHealth() {
	if sess == nil || sess.client == nil || sess.healthInput == nil {
		return
	}

	health := buildHealth(sess.healthInput(), sess.redact)
	fingerprint := healthFingerprint(health)
	now := time.Now()

	sess.healthMu.Lock()
	changed := fingerprint != sess.lastFingerprint
	heartbeatElapsed := now.Sub(sess.lastHealthPush) >= sess.heartbeat
	if !changed && !heartbeatElapsed {
		sess.healthMu.Unlock()
		return
	}
	sess.lastFingerprint = fingerprint
	sess.lastHealthPush = now
	sess.healthMu.Unlock()

	if err := sess.client.SetHealth(health); err != nil {
		sess.reportSetError("OpAMP health report failed", err)
		return
	}
	agentstate.IncrementOpAMPMessagesSent()
}

func (sess *session) reportSetError(msg string, err error) {
	agentstate.IncrementOpAMPErrors(agentstate.OpAMPErrorReport)
	sess.log.Debug().Str("error", sess.redact(err.Error())).Msg(msg)
}

func (sess *session) healthLoop(ctx context.Context) {
	if sess == nil {
		return
	}
	defer close(sess.healthDone)

	ticker := time.NewTicker(sess.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sess.healthStop:
			return
		case <-ticker.C:
			sess.pushHealth()
		}
	}
}

func (sess *session) stop(ctx context.Context) {
	if sess == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sess.healthOnce.Do(func() { close(sess.healthStop) })

	if sess.client != nil {
		if err := sess.client.Stop(ctx); err != nil {
			sess.log.Debug().
				Str("error", sess.redact(err.Error())).
				Msg("OpAMP client stop reported an error")
		}
	}

	if sess.healthStarted {
		select {
		case <-sess.healthDone:
		case <-ctx.Done():
		}
	}
}

func (sess *session) connected() bool {
	sess.connMu.Lock()
	defer sess.connMu.Unlock()
	return sess.connState == connConnected
}

func (sess *session) onConnect(_ context.Context) {
	agentstate.SetOpAMPConnected(true)

	sess.connMu.Lock()
	previous := sess.connState
	sess.connState = connConnected
	sess.connMu.Unlock()

	switch previous {
	case connNever:
		sess.log.Info().
			Str("endpoint", configuration.RedactEndpoint(sess.cfg.Endpoint)).
			Str("transport", sess.cfg.Transport).
			Msg("OpAMP connected")
	case connDisconnected:
		sess.log.Info().Msg("OpAMP reconnected")
	}
}

// firstFailureMessage names the likely cause of a first failed connection.
// opamp-go does not surface the HTTP status of a refused WebSocket upgrade,
// only "bad handshake", which is also what a server answering 401 or 403 to
// the token or the tenant's agreement produces; calling that "unreachable"
// sent operators looking for a network fault.
func firstFailureMessage(err string) string {
	if strings.Contains(err, "bad handshake") {
		return "OpAMP server refused the connection (a rejected token or agreement looks like this) or is not an OpAMP endpoint; retrying with backoff"
	}
	return "OpAMP server unreachable; retrying with backoff"
}

func (sess *session) onConnectFailed(_ context.Context, err error) {
	agentstate.SetOpAMPConnected(false)
	agentstate.IncrementOpAMPErrors(agentstate.OpAMPErrorConnect)

	redacted := sess.redact(err.Error())

	sess.connMu.Lock()
	previous := sess.connState
	warn := false
	msg := ""
	switch previous {
	case connConnected:
		sess.connState = connDisconnected
		msg = "OpAMP connection lost; retrying with backoff"
		warn = true
	case connNever:
		if !sess.firstFailureLogged {
			sess.firstFailureLogged = true
			msg = firstFailureMessage(redacted)
			warn = true
		}
	}
	sess.connMu.Unlock()

	if warn {
		sess.log.Warn().Str("error", redacted).Msg(msg)
	} else {
		sess.log.Debug().Str("error", redacted).Msg("OpAMP connection attempt failed")
	}
}
