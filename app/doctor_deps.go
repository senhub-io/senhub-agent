package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kardianos/service"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/disk"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/configuration/secret"
	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/otlp"
	"senhub-agent.go/internal/agent/services/license"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
)

const doctorOutputTimeout = 8 * time.Second

// installProbe holds the host facts the install section reads. A nil
// function means the host cannot answer, and the check is skipped.
type installProbe struct {
	unit         func() (string, error)
	binaryExists func(path string) bool
	mainPID      func() (string, error)
	exeLink      func(pid string) (string, error)
	sameContents func(a, b string) (bool, error)
	logGroup     string
	groupMember  func(user string) (member, groupExists bool, err error)
	logDirAccess func(dir, serviceUser string) (logDirState, error)
	serviceBin   func() string
	binVersion   func(path string) string
}

type logDirState struct {
	Exists   bool
	Writable bool
	Detail   string
}

// doctorDeps is everything doctor reads from the machine, so each
// section can be exercised with fakes.
type doctorDeps struct {
	configPath    string
	now           func() time.Time
	cliVersion    string
	logDir        string
	configCheck   func(path string) (string, checkOutcome, error)
	loadConfig    func(path string) (configuration.LocalConfigurationData, error)
	status        func() statusResult
	httpListen    func(path string) (scheme, bind string, port int)
	portFree      func(bind string, port int) error
	secretStore   func(configDir string) (secretReport, error)
	licenseExpiry func(token string) (tier string, expires time.Time, err error)
	outputTest    func(ctx context.Context, outputType string, params map[string]interface{}, agentKey string, port int) []otlp.ConnectionStep
	freePercent   func(path string) (float64, error)
	install       installProbe
}

// secretReport is what `secret status` says, or that no store exists.
type secretReport struct {
	Present bool
	Backend string
	Store   string
	Secrets int
}

func newDoctorDeps(configPath string) doctorDeps {
	return doctorDeps{
		configPath: configPath,
		now:        time.Now,
		cliVersion: cliArgs.Version,
		logDir:     agentLogger.LogBaseDir(),
		configCheck: func(path string) (string, checkOutcome, error) {
			var outcome checkOutcome
			text, err := captureOutput(func() { outcome = checkConfig(path) })
			return text, outcome, err
		},
		loadConfig: func(path string) (configuration.LocalConfigurationData, error) {
			zlog := zerolog.New(os.Stderr).Level(zerolog.ErrorLevel)
			log := agentLogger.NewModuleLogger((*agentLogger.Logger)(&zlog), "configuration.doctor")
			return configuration.LoadFromDisk(path, log)
		},
		status:     func() statusResult { return collectDoctorStatus(configPath) },
		httpListen: resolveHTTPStrategyListen,
		portFree:   checkHTTPPortFree,
		secretStore: func(dir string) (secretReport, error) {
			return readSecretStatus(dir)
		},
		outputTest: func(ctx context.Context, outputType string, params map[string]interface{}, agentKey string, port int) []otlp.ConnectionStep {
			return http.RunOutputConnectionTest(ctx, outputType, params, doctorOutputTimeout, agentKey, port)
		},
		licenseExpiry: func(token string) (string, time.Time, error) {
			validator, err := license.GetDefaultValidator(7)
			if err != nil {
				return "", time.Time{}, fmt.Errorf("building the licence validator: %w", err)
			}
			lic, err := validator.ValidateLicense(token)
			if err != nil {
				return "", time.Time{}, fmt.Errorf("validating the licence: %w", err)
			}
			return string(lic.Tier), lic.ExpiresAt, nil
		},
		freePercent: diskFreePercent,
		install:     platformInstallProbe(),
	}
}

// collectDoctorStatus is `status` without its printing: the service
// manager's answer first, then the running agent's own over HTTP.
func collectDoctorStatus(configPath string) statusResult {
	args := &cliArgs.ParsedArgs{ConfigPath: configPath}
	svc, err := service.New(&program{done: make(chan bool, 1), args: args}, &service.Config{
		Name:        "senhub-agent",
		DisplayName: "SenHub Agent",
	})
	if err != nil {
		return statusResult{managerErr: err, source: ""}
	}
	nop := zerolog.Nop()
	return collectStatusWith(svc, args, (*agentLogger.Logger)(&nop))
}

var errNoSecretStore = errors.New("no secret store")

// secretStorePresent reports whether dir holds any secret store file.
func secretStorePresent(dir string) bool {
	for _, name := range []string{"secrets.age", "agent-secret.key", "creds.d", "secrets.dpapi", "entropy.bin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// readSecretStatus runs `secret status` against dir and reads back its
// JSON document. A directory with no store is reported as such rather
// than opened: opening one creates the key file.
func readSecretStatus(dir string) (secretReport, error) {
	if !secretStorePresent(dir) {
		return secretReport{}, nil
	}
	if err := secret.InitRegistry(dir); err != nil {
		return secretReport{}, fmt.Errorf("initialising secret backend: %w", err)
	}
	p := secret.ActiveProvider()
	if p == nil {
		return secretReport{}, errNoSecretStore
	}
	var buf bytes.Buffer
	runSecretStatus(p, dir, true, &buf)
	var doc secretStatusReport
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		return secretReport{}, fmt.Errorf("reading the secret status: %w", err)
	}
	if doc.Error != "" {
		return secretReport{}, errors.New(doc.Error)
	}
	return secretReport{Present: true, Backend: doc.Backend, Store: doc.Store, Secrets: doc.Secrets}, nil
}

// diskFreePercent is the share of the volume holding path that is free.
func diskFreePercent(path string) (float64, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return 0, fmt.Errorf("reading the volume of %s: %w", path, err)
	}
	if usage.Total == 0 {
		return 0, fmt.Errorf("the volume of %s reports no capacity", path)
	}
	return float64(usage.Free) / float64(usage.Total) * 100, nil
}
