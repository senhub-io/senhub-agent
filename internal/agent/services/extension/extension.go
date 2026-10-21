// Package extension lets a build add an agent-lifetime service the core
// does not know about. An extension registers itself, typically from an
// init function; the agent builds it once at start with a Host that gives
// it the agent's identity, its configuration block, its health and its
// redacted configuration, and the supervisor starts and stops it like the
// built-in services. With nothing registered, the agent runs as before.
package extension

import (
	"context"
	"sync"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// Service is what an extension runs for the agent's lifetime. Start must
// not block and must not fail on a configuration problem: an error from
// Start stops the whole agent, which is reserved for a fault the agent
// cannot run with.
type Service interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Extension describes one registered extension.
type Extension struct {
	// Name identifies the extension in logs, in the status channel and
	// as the default doctor check ID. Unique.
	Name string
	// ConfigBlock is the top-level key of agent.yaml this extension
	// reads; empty when it reads none. Unique among extensions. The
	// block is loaded, substituted, shown by `config show` and watched
	// like the built-in blocks.
	ConfigBlock string
	// New builds the service. It is called once per agent start.
	New func(Host) (Service, error)
	// StopBudget is the time Shutdown may take; zero means the
	// supervisor's default.
	StopBudget time.Duration
	// Check adds lines to `config check` for the block. present is false
	// when the configuration has no such block.
	Check func(block map[string]any, present bool) []CheckLine
	// Doctor adds one line to `doctor`. Nil adds none.
	Doctor func(DoctorInput) DoctorLine
	// Overlay adjusts the configuration each time the running agent
	// loads it, after the files are read and before the result is
	// validated and published. The files are never rewritten. Nil
	// changes nothing.
	Overlay func(OverlayTarget)
}

// Host is the agent as an extension sees it.
type Host interface {
	// Logger is the agent's logger.
	Logger() *logger.Logger
	// ConfigBlock returns a copy of the named top-level block of the
	// configuration, references substituted. Only a block an extension
	// declared as its ConfigBlock is kept by the loader.
	ConfigBlock(name string) (map[string]any, bool)
	// OnConfigChange registers fn, called after each configuration
	// reload that changed something.
	OnConfigChange(fn func())
	// Identity returns what the agent is.
	Identity() Identity
	// HostAttributes returns the host resource attributes the OTLP
	// output attaches to its resource.
	HostAttributes() (map[string]string, error)
	// Authorized reports whether the licence allows the named feature.
	Authorized(feature string) bool
	// Health returns the agent's current health.
	Health() Health
	// EffectiveConfig returns the redacted configuration file by file,
	// as `config show --redact` masks it.
	EffectiveConfig() ([]ConfigFile, error)
	// PublishStatus replaces what the status channel reports for this
	// extension; doctor receives it as DoctorInput.Status.
	PublishStatus(status map[string]any)
	// ReloadConfig reads the configuration again, overlays included, and
	// applies it as a change on disk would. On error the configuration
	// in force is kept.
	ReloadConfig() error
	// SetLogLevel sets the level of every module logger: debug, info,
	// warn or error. An empty level puts back the levels the agent
	// started with.
	SetLogLevel(level string) error
}

// Identity is what the agent is.
type Identity struct {
	// InstanceID is service.instance.id.
	InstanceID string
	Version    string
	// Edition is "enterprise" when the build carries a probe of the paid
	// catalogue, "oss" otherwise.
	Edition string
	// LicenseTier is the tier of the licence in force: "free" without a
	// valid one.
	LicenseTier string
	// LicenseID is the jti of the licence in force; empty without one.
	LicenseID string
	// LicenseExpiresAt is when the licence in force expires; zero
	// without one.
	LicenseExpiresAt time.Time
}

// Health is the agent's current health, from the state status and doctor
// read.
type Health struct {
	// Healthy is false when an enabled probe or an output has failed.
	Healthy   bool
	StartTime time.Time
	Probes    []ProbeHealth
	Outputs   []OutputHealth
}

// ProbeHealth is one configured probe.
type ProbeHealth struct {
	Name    string
	Enabled bool
	Running bool
	// Health is "ok", "failed", "unknown", or "" when the probe is not
	// running and has not failed to start.
	Health    string
	LastError string
	// StartedAt is when the probe joined the running set; zero when it
	// is not running.
	StartedAt time.Time
	LastCycle time.Time
}

// OutputHealth is one configured output.
type OutputHealth struct {
	Name string
	// FailureReason and FailureDetail are set when the output is not
	// running.
	FailureReason string
	FailureDetail string
	// Failing is true while the output's exports fail; LastError is the
	// latest export error.
	Failing   bool
	LastError string
}

// ConfigFile is one loaded configuration file, redacted.
type ConfigFile struct {
	// Name is relative to the configuration directory and
	// slash-separated; "environment" holds the probes the environment
	// declares.
	Name string
	Body []byte
}

// Check line levels for `config check`.
const (
	CheckOK    = "ok"
	CheckOff   = "off"
	CheckWarn  = "warn"
	CheckError = "error"
)

// CheckLine is one `config check` line.
type CheckLine struct {
	Level string
	Text  string
}

// Doctor line levels.
const (
	DoctorOK   = "ok"
	DoctorWarn = "warn"
	DoctorFail = "fail"
	DoctorSkip = "skip"
)

// DoctorInput is what doctor knows when it asks an extension for its line.
type DoctorInput struct {
	// ConfigErr is set when doctor could not load the configuration.
	ConfigErr error
	// Block is the extension's configuration block; Present is false
	// when there is none.
	Block   map[string]any
	Present bool
	// AgentAnswered is true when the running agent answered the status
	// request; Status is what the extension published there, nil when it
	// published nothing.
	AgentAnswered bool
	Status        map[string]any
}

// DoctorLine is one doctor line. ID defaults to "config.<Name>".
type DoctorLine struct {
	ID      string
	Level   string
	Message string
	Fix     string
}

var (
	mu         sync.RWMutex
	registered []Extension
	statuses   = map[string]map[string]any{}
)

// Register adds an extension. An extension with an empty name, a nil New,
// or a name or block already taken is not kept; Problems says why.
func Register(e Extension) {
	mu.Lock()
	defer mu.Unlock()
	registered = append(registered, e)
}

// Registered returns the valid extensions, in registration order.
func Registered() []Extension {
	valid, _ := split()
	return valid
}

// Problems describes each registration Registered left out.
func Problems() []string {
	_, problems := split()
	return problems
}

func split() ([]Extension, []string) {
	mu.RLock()
	defer mu.RUnlock()
	var valid []Extension
	var problems []string
	names := map[string]bool{}
	blocks := map[string]bool{}
	for _, e := range registered {
		switch {
		case e.Name == "":
			problems = append(problems, "an extension has no name")
		case e.New == nil:
			problems = append(problems, "extension "+e.Name+" has no constructor")
		case names[e.Name]:
			problems = append(problems, "extension "+e.Name+" is registered twice")
		case e.ConfigBlock != "" && blocks[e.ConfigBlock]:
			problems = append(problems, "extension "+e.Name+" claims block "+e.ConfigBlock+", already claimed")
		default:
			names[e.Name] = true
			if e.ConfigBlock != "" {
				blocks[e.ConfigBlock] = true
			}
			valid = append(valid, e)
		}
	}
	return valid, problems
}

// ConfigBlocks returns the configuration blocks the valid extensions
// claim.
func ConfigBlocks() map[string]bool {
	out := map[string]bool{}
	for _, e := range Registered() {
		if e.ConfigBlock != "" {
			out[e.ConfigBlock] = true
		}
	}
	return out
}

// SetStatus records what the status channel reports for an extension;
// nil removes it.
func SetStatus(name string, status map[string]any) {
	mu.Lock()
	defer mu.Unlock()
	if status == nil {
		delete(statuses, name)
		return
	}
	cp := make(map[string]any, len(status))
	for k, v := range status {
		cp[k] = v
	}
	statuses[name] = cp
}

// Statuses returns a copy of every published status, nil when none.
func Statuses() map[string]map[string]any {
	mu.RLock()
	defer mu.RUnlock()
	if len(statuses) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(statuses))
	for name, st := range statuses {
		cp := make(map[string]any, len(st))
		for k, v := range st {
			cp[k] = v
		}
		out[name] = cp
	}
	return out
}

// ResetForTest clears every registration and status. Tests only.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	registered = nil
	statuses = map[string]map[string]any{}
}
