// Debug and status utilities
package app

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kardianos/service"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/probes/spec"
	"senhub-agent.go/internal/agent/services/configuration"
	agentLogger "senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
	"senhub-agent.go/internal/cliexit"
)

func showDebugModules() {
	exe := os.Args[0]

	fmt.Println("Available debug filters:")
	fmt.Println()
	// The probe list is read from the registry rather than kept by hand.
	// The hand-written one named twelve types out of sixty-six, advertised
	// three prefixes no module ever used, and could only fall further
	// behind: a filter that selects nothing looks like a silent agent.
	fmt.Println("  Probes:")
	fmt.Println("    probe                 Every probe")
	for _, ps := range spec.Registered() {
		name := ps.DisplayName
		if name == "" {
			name = ps.Type
		}
		fmt.Printf("    %-21s %s\n", "probe."+ps.Type, name)
	}
	fmt.Println()
	fmt.Println("  Agent:")
	fmt.Println("    sensor                Probe lifecycle management")
	fmt.Println("    configuration         Configuration loading & watching")
	fmt.Println("    strategy              All output strategies (http, prtg, senhub)")
	fmt.Println("    strategy.http         HTTP API & web UI")
	fmt.Println("    transformer           Metric transformation & lookups")
	fmt.Println("    data_store            Data routing")
	fmt.Println("    service.auto_update   Auto-update system")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s run --filter probe.veeam              # one probe\n", exe)
	fmt.Printf("  %s run --filter probe                    # all probes\n", exe)
	fmt.Printf("  %s run --filter probe.veeam,strategy.http # combine filters\n", exe)
	fmt.Println()
}

// statusResult is everything `status` learned, before any of it is
// rendered: the text and JSON views both read from it, so they cannot
// disagree about what was found.
type statusResult struct {
	// serviceState is the service manager's answer, "" when there is no
	// manager to ask (a container).
	serviceState string
	managerErr   error
	// notRunning is true when the manager says the service is not running.
	notRunning bool
	// source says where the status came from: "daemon" (the running agent
	// answered over HTTP), "local" (computed by this process) or "minimal"
	// (even that failed). Empty when the service is not running.
	source string
	system status.SystemStatus
	// notice is the explanation printed before a local view that stands
	// in for the daemon's.
	notice   string
	otlp     *status.OTLPInfo
	otlpErr  error
	localErr error
}

// exitCode: a stopped service, a daemon that did not answer, or an agent
// that reports itself unhealthy, with a dead output or a probe in error
// all need attention but do not make the status query itself fail.
func (r statusResult) exitCode() int {
	switch {
	case r.notRunning || r.source != "daemon":
		return cliexit.Warning
	case r.system.Health.Status != "healthy" || len(r.system.StrategyFailures) > 0:
		return cliexit.Warning
	case r.system.ProbesError != "" || anyProbeInError(r.system.Probes):
		return cliexit.Warning
	default:
		return cliexit.OK
	}
}

func anyProbeInError(probes []status.ProbeStatus) bool {
	for _, p := range probes {
		if p.Status == "error" {
			return true
		}
	}
	return false
}

// collectStatus gathers the status without printing anything.
func collectStatus(svc service.Service, args *cliArgs.ParsedArgs) statusResult {
	return collectStatusWith(svc, args, agentLogger.NewLogger(&cliArgs.ParsedArgs{Verbose: false}))
}

// collectStatusWith is collectStatus with the logger the caller chooses,
// so a read-only command can keep the file logger from opening.
func collectStatusWith(svc service.Service, args *cliArgs.ParsedArgs, logger *agentLogger.Logger) statusResult {
	var res statusResult
	statusHelper := status.NewStatusHelper(logger)

	serviceStatus, err := statusHelper.GetServiceStatus(svc)
	if err != nil {
		res.managerErr = err
	} else {
		res.serviceState = serviceStatus
		if serviceStatus != "running" {
			res.notRunning = true
			return res
		}
	}

	// The authentication key always comes from the configuration file
	// in 0.2.0+ — the CLI flag was removed with the legacy remote-config loader.
	agentKey := ""
	// Why the running agent could not be asked. Printed before the local
	// view, which otherwise reads as the agent's own answer.
	keyProblem := ""
	reachProblem := ""
	if args != nil {
		// Use absolute path based on binary location (fixes Windows Service issue)
		configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
		if err != nil {
			// Fallback to provided path if absolute path resolution fails
			configPath = args.ConfigPath
			if configPath == "" {
				configPath = "./agent-config.yaml"
			}
		}

		if extractedKey, err := extractAgentKeyFromConfig(configPath); err == nil {
			agentKey = extractedKey
		} else {
			keyProblem = fmt.Sprintf("the agent key could not be read from %s (%v)", configPath, err)
		}
	}

	// Resolve config path once for reuse
	configPath := ""
	if args != nil {
		if resolved, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath); err == nil {
			configPath = resolved
		}
	}

	// Try HTTP endpoint first (for running agent with HTTP strategy)
	if agentKey != "" {
		scheme, bind, httpPort := resolveHTTPStrategyListen(configPath)
		host := localHostFor(bind)
		statusHelper.SetEndpoint(scheme, host)
		systemStatus, err := statusHelper.GetDetailedStatusFromHTTP(agentKey, httpPort)
		if err != nil {
			reachProblem = fmt.Sprintf("the running agent did not answer at %s://%s (%v)", scheme, net.JoinHostPort(host, strconv.Itoa(httpPort)), err)
		}
		if err == nil {
			// Enrich with dashboard URL from config
			if configPath != "" {
				systemStatus.Connection.DashboardURL = consoleHint()
			}
			res.source = "daemon"
			res.system = *systemStatus

			// --otlp adds an OTLP self-metric block after the standard view.
			// Failure here is non-fatal: the standard status already printed.
			if args != nil && args.ShowOTLP {
				res.otlp, res.otlpErr = statusHelper.GetOTLPInfoFromHTTP(agentKey, httpPort)
			}
			return res
		}
		// HTTP failed, fall back to direct method
		// Note: this happens when the HTTP strategy is not enabled, or the
		// agent is not listening on the resolved port
	}

	// The local view describes this process, not the daemon: it cannot
	// say which outputs are running or whether the configuration is
	// watched. Saying so, and why, is the difference between a degraded
	// answer and a wrong one.
	res.notice = daemonUnreachableNotice(keyProblem, reachProblem)

	// Fallback: Get system status directly using StatusService (no HTTP dependency)
	systemStatus, err := getSystemStatusWith(args, logger)
	if err != nil {
		res.localErr = err
		res.source = "minimal"
		res.system = status.SystemStatus{
			Health: status.HealthInfo{
				Status:    "unknown",
				Timestamp: time.Now(),
				Message:   "Service is running but status unavailable",
			},
			Agent: status.AgentInfo{
				Version:   "unknown",
				Commit:    "unknown",
				GoVersion: runtime.Version(),
				OS:        runtime.GOOS,
				Arch:      runtime.GOARCH,
			},
		}
		return res
	}
	res.source = "local"
	res.system = systemStatus
	return res
}

// renderStatusText prints the status the way `status` always has.
func renderStatusText(res statusResult, out io.Writer) {
	formatter := status.NewCLIFormatter()

	if res.managerErr != nil {
		// No service manager to ask: a container, where the agent is the
		// container's own process. The running agent can still answer for
		// itself over HTTP, which is what an operator ran status for.
		fmt.Fprintf(out, "Service status: no service manager here (%v); asking the running agent\n\n", res.managerErr)
	} else {
		// Capitalize first letter for display
		displayStatus := strings.ToUpper(res.serviceState[:1]) + res.serviceState[1:]
		fmt.Fprintf(out, "Service status: %s\n\n", displayStatus)

		// If service is not running, show basic info only
		if res.notRunning {
			fmt.Fprintln(out, "Agent service is not running.")
			fmt.Fprintln(out, "Start the service with: "+os.Args[0]+" start")
			return
		}
	}

	switch res.source {
	case "daemon":
		fmt.Fprint(out, formatter.FormatSystemStatus(res.system))
		if res.otlp != nil {
			fmt.Fprint(out, "\n")
			fmt.Fprint(out, formatter.FormatOTLPInfo(res.otlp))
		} else if res.otlpErr != nil {
			fmt.Fprintf(os.Stderr, "\nNote: could not fetch OTLP info (%v)\n", res.otlpErr)
		}
	case "minimal":
		if res.notice != "" {
			fmt.Fprint(out, res.notice)
		}
		fmt.Fprintf(os.Stderr, "Note: Could not get system status (%v), showing minimal status\n\n", res.localErr)
		fmt.Fprint(out, formatter.FormatBasicStatus(res.system.Health, res.system.Agent))
	default:
		if res.notice != "" {
			fmt.Fprint(out, res.notice)
		}
		fmt.Fprint(out, formatter.FormatSystemStatus(res.system))
	}
}

type statusReport struct {
	jsonHeader
	Service statusServiceInfo    `json:"service"`
	Source  string               `json:"source"`
	Notice  string               `json:"notice,omitempty"`
	Agent   *status.SystemStatus `json:"agent,omitempty"`
	OTLP    *status.OTLPInfo     `json:"otlp,omitempty"`
}

type statusServiceInfo struct {
	State            string `json:"state"`
	ManagerAvailable bool   `json:"manager_available"`
	Detail           string `json:"detail,omitempty"`
}

// runStatus implements `status [--otlp] [--json]` and returns the exit
// code.
func runStatus(svc service.Service, args *cliArgs.ParsedArgs, jsonMode bool, out io.Writer) int {
	res := collectStatus(svc, args)
	code := res.exitCode()
	if !jsonMode {
		renderStatusText(res, out)
		return code
	}

	report := statusReport{
		jsonHeader: newJSONHeader("status", code),
		Service: statusServiceInfo{
			State:            res.serviceState,
			ManagerAvailable: res.managerErr == nil,
		},
		Source: res.source,
		Notice: strings.TrimSpace(res.notice),
		OTLP:   res.otlp,
	}
	if res.managerErr != nil {
		report.Service.State = "unknown"
		report.Service.Detail = res.managerErr.Error()
	}
	if res.notRunning {
		report.Source = "none"
	} else {
		sys := res.system
		report.Agent = &sys
	}
	if err := writeJSON(out, report); err != nil {
		return reportFailure("status", false, out, err)
	}
	return code
}

// getSystemStatusDirect gets system status directly using StatusService (no HTTP dependency)
func getSystemStatusDirect(args *cliArgs.ParsedArgs) (status.SystemStatus, error) {
	return getSystemStatusWith(args, agentLogger.NewLogger(&cliArgs.ParsedArgs{Verbose: false}))
}

func getSystemStatusWith(args *cliArgs.ParsedArgs, logger *agentLogger.Logger) (status.SystemStatus, error) {
	// Handle nil args case
	if args == nil {
		args = &cliArgs.ParsedArgs{}
	}

	// Try to get version and commit information
	version := cliArgs.Version
	commit := cliArgs.CommitHash
	if version == "" {
		version = "development"
	}

	// Format commit hash for display (take first 8 chars if longer than 8 and looks like a git hash)
	// Avoid truncating non-hash values like "latest-dev"
	if len(commit) > 8 && isGitHash(commit) {
		commit = commit[:8]
	}

	// Create status service
	statusService := status.NewStatusService(logger, version, commit)

	// Note: Without actual probe/cache data, we'll get basic system info
	// In a real deployment, this would connect to the running agent's internal state
	systemStatus := statusService.GetSystemStatus()

	// Try to enhance with the agent key extracted from the config file.
	if args != nil {
		agentKey := ""

		{
			// Use absolute path based on binary location (fixes Windows Service issue)
			configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
			if err != nil {
				// Fallback to provided path if absolute path resolution fails
				configPath = args.ConfigPath
				if configPath == "" {
					configPath = "./agent-config.yaml"
				}
			}

			if extractedKey, err := extractAgentKeyFromConfig(configPath); err == nil {
				agentKey = extractedKey
			}
		}

		// Update connection info with agent key source
		if agentKey != "" {
			systemStatus.Connection.Source = "Configuration file"
			systemStatus.Connection.Status = "Available"

			systemStatus.Connection.DashboardURL = consoleHint()
		}
	}

	return systemStatus, nil
}

// consoleHint is what status and help show for the console. The console
// answers the administration key only, so an address built on the agent
// key answered 401; and the administration key does not belong in output
// that is routinely pasted into a ticket. The command prints the address
// to whoever has the rights to read it.
func consoleHint() string {
	return "run '" + filepath.Base(os.Args[0]) + " console' (or 'console --print' for the address)"
}

// buildDashboardURL constructs the dashboard URL from the agent
// configuration, whichever layout it uses.
func buildDashboardURL(configPath string, agentKey string) string {
	if agentKey == "" {
		return ""
	}
	scheme, port := resolveHTTPStrategyEndpoint(configPath)
	return fmt.Sprintf("%s://localhost:%d/web/%s/dashboard", scheme, port, agentKey)
}

// isGitHash checks if a string looks like a git commit hash (hex characters only)
func isGitHash(s string) bool {
	if len(s) < 7 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// validateConfigPath validates that the config path is safe to read

// resolveHTTPStrategyPort finds the port the running agent's HTTP
// strategy listens on, so `status` reaches a daemon configured on
// anything other than the default.
func resolveHTTPStrategyPort(configPath string) int {
	_, port := resolveHTTPStrategyEndpoint(configPath)
	return port
}

// resolveHTTPStrategyEndpoint returns the scheme and port of the HTTP
// strategy as configured on disk, falling back to plain HTTP on 8080
// when the configuration cannot be read.
//
// It goes through the real configuration loader rather than parsing the
// main file by hand: an operator running the multi-file layout keeps the
// http strategy in strategies.d/, invisible to a `strategies:` lookup in
// agent.yaml. Status silently fell back to the degraded local view on
// every such host, which also hid the dead-output report (#826), and
// every printed console address named 8080 whatever the file said.
func resolveHTTPStrategyEndpoint(configPath string) (scheme string, port int) {
	scheme, _, port = resolveHTTPStrategyListen(configPath)
	return scheme, port
}

// localHostFor turns the output's bind address into the host a command on
// this machine dials: the address itself when the output is bound to one,
// loopback when it listens on every address or on none in particular.
func localHostFor(bind string) string {
	switch bind {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	}
	return bind
}

// resolveHTTPStrategyListen is resolveHTTPStrategyEndpoint plus the bind
// address, which a command on this machine must dial when the output is
// bound to one interface (#969).
func resolveHTTPStrategyListen(configPath string) (scheme, bind string, port int) {
	scheme, port = "http", defaultHTTPPort
	if configPath == "" {
		return scheme, bind, port
	}
	cfg, err := configuration.LoadFromDisk(configPath, nil)
	if err != nil {
		return scheme, bind, port
	}
	for _, storage := range cfg.Storage {
		if storage.Name != "http" {
			continue
		}
		switch v := storage.Params["port"].(type) {
		case int:
			if v > 0 {
				port = v
			}
		case float64:
			if v > 0 {
				port = int(v)
			}
		}
		if tlsEnabledParam(storage.Params["tls"]) {
			scheme = "https"
		}
		if b, ok := storage.Params["bind_address"].(string); ok {
			bind = b
		}
		return scheme, bind, port
	}
	return scheme, bind, port
}

// tlsEnabledParam reads `tls.enabled` from a strategy parameter block.
// The block arrives with string keys from the runtime parser and with
// interface keys straight from the yaml.v2 loader; both are read.
func tlsEnabledParam(v interface{}) bool {
	switch m := v.(type) {
	case map[string]interface{}:
		enabled, _ := m["enabled"].(bool)
		return enabled
	case map[interface{}]interface{}:
		enabled, _ := m["enabled"].(bool)
		return enabled
	}
	return false
}

// daemonUnreachableNotice explains why the local view is about to be
// printed instead of the running agent's own state. Empty when the
// daemon answered. The key problem comes first: it is the one the
// operator can act on, and it is what makes the port unreachable.
func daemonUnreachableNotice(keyProblem, reachProblem string) string {
	why := keyProblem
	if why == "" {
		why = reachProblem
	}
	if why == "" {
		return ""
	}
	return fmt.Sprintf("The running agent could not be asked: %s.\nWhat follows is what this command sees on its own. It is not the service's state: it cannot say which outputs are running, nor whether the configuration is watched.\n\n", why)
}
