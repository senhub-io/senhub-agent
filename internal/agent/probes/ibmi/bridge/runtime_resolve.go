package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RuntimeMode names the kind of subprocess the bridge will spawn.
type RuntimeMode string

const (
	// RuntimeModeNative — a self-contained GraalVM native-image
	// binary (`jt400runner` / `jt400runner.exe`). No JRE on the host.
	RuntimeModeNative RuntimeMode = "native"
	// RuntimeModeJava — the legacy `java -cp jt400.jar:. Jt400Runner`
	// path. Requires a JRE.
	RuntimeModeJava RuntimeMode = "java"
)

// Resolution is the outcome of the runtime-resolution search.
// On success, Mode is set and the relevant path field (NativeRunner
// or JavaBin + RunnerDir) is populated. On failure, Err is non-nil
// and Tried lists every candidate the resolver looked at — so the
// operator can see exactly which paths were attempted.
type Resolution struct {
	Mode         RuntimeMode
	NativeRunner string   // populated when Mode == RuntimeModeNative
	JavaBin      string   // populated when Mode == RuntimeModeJava
	RunnerDir    string   // populated when Mode == RuntimeModeJava (contains jt400.jar + Jt400Runner.class)
	Source       string   // human-readable origin: "config:native_runner", "auto-detect", "JAVA_HOME env"
	Tried        []string // every candidate the resolver examined, in order
	Err          error    // non-nil when no runtime could be located
}

// ResolveRuntime walks the priority list and returns the first
// candidate that exists on disk. Priority order:
//
//  1. `cfg.NativeRunner` — explicit config (highest precedence; never
//     auto-overridden).
//  2. `<agent-binary-dir>/bridge/jt400runner[.exe]` — auto-detect
//     when the native binary is shipped alongside the agent.
//  3. `cfg.JavaHome/bin/java[.exe]` + `cfg.RunnerDir` — explicit JRE
//     config.
//  4. `os.Getenv("JAVA_HOME")/bin/java[.exe]` + `cfg.RunnerDir` —
//     fall back to the standard env variable.
//
// The function only checks existence (os.Stat); whether the binary
// is *invokable* is the bridge's concern at spawn time. We don't
// try to exec it here — that's the caller's choice (a smoke test
// for `senhub-agent ibmi check` may want to, the hot Collect path
// does not).
func ResolveRuntime(cfg Config) Resolution {
	res := Resolution{}

	// 1. Explicit NativeRunner from config.
	if p := strings.TrimSpace(cfg.NativeRunner); p != "" {
		res.Tried = append(res.Tried, p+" (config: bridge.native_runner)")
		if _, err := os.Stat(p); err == nil {
			res.Mode = RuntimeModeNative
			res.NativeRunner = p
			res.Source = "config: bridge.native_runner"
			res.RunnerDir = cfg.RunnerDir // optional working directory
			return res
		}
	}

	// 2. Auto-detect sibling native runner next to the senhub-agent binary.
	if sibling := siblingNativeRunner(); sibling != "" {
		label := sibling + " (auto-detect: next to senhub-agent)"
		res.Tried = append(res.Tried, label)
		if _, err := os.Stat(sibling); err == nil {
			res.Mode = RuntimeModeNative
			res.NativeRunner = sibling
			res.Source = "auto-detect: sibling to senhub-agent"
			res.RunnerDir = cfg.RunnerDir
			return res
		}
	}

	// 3. Explicit JavaHome from config.
	if jh := strings.TrimSpace(cfg.JavaHome); jh != "" {
		bin := javaBinaryUnder(jh)
		res.Tried = append(res.Tried, bin+" (config: bridge.java_home)")
		if _, err := os.Stat(bin); err == nil {
			if cfg.RunnerDir == "" {
				res.Err = fmt.Errorf("found %s but bridge.runner_dir is unset — JRE mode needs the directory holding jt400.jar + Jt400Runner.class", bin)
				return res
			}
			res.Mode = RuntimeModeJava
			res.JavaBin = bin
			res.RunnerDir = cfg.RunnerDir
			res.Source = "config: bridge.java_home"
			return res
		}
	}

	// 4. JAVA_HOME env fallback.
	if jh := strings.TrimSpace(os.Getenv("JAVA_HOME")); jh != "" {
		bin := javaBinaryUnder(jh)
		res.Tried = append(res.Tried, bin+" (env: JAVA_HOME)")
		if _, err := os.Stat(bin); err == nil {
			if cfg.RunnerDir == "" {
				res.Err = fmt.Errorf("JAVA_HOME=%s points at a usable JRE but bridge.runner_dir is unset — JRE mode needs the directory holding jt400.jar + Jt400Runner.class", jh)
				return res
			}
			res.Mode = RuntimeModeJava
			res.JavaBin = bin
			res.RunnerDir = cfg.RunnerDir
			res.Source = "env: JAVA_HOME"
			return res
		}
	}

	// None of the candidates worked. Build a single error that names
	// every path we tried so the operator can fix the right one.
	res.Err = noRuntimeError(res.Tried)
	return res
}

// siblingNativeRunner returns the auto-detect candidate path for a
// jt400runner native binary next to the senhub-agent executable.
// Returns "" when the agent's own path can't be determined (very
// unusual — only happens if os.Executable fails, which means the
// caller can fall through to the JRE path).
func siblingNativeRunner() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	name := "jt400runner"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, "bridge", name)
}

// javaBinaryUnder returns the conventional path to the `java`
// executable under a JAVA_HOME-style directory, OS-aware.
func javaBinaryUnder(javaHome string) string {
	bin := filepath.Join(javaHome, "bin", "java")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	return bin
}

// noRuntimeError formats the final error when every candidate failed.
// The message names every path tried plus the remediation path so
// the operator never has to guess what we looked for.
func noRuntimeError(tried []string) error {
	var sb strings.Builder
	sb.WriteString("no IBM i runtime found.\n")
	if len(tried) > 0 {
		sb.WriteString("  Paths tried:\n")
		for _, t := range tried {
			sb.WriteString("    - ")
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("  No candidates configured (neither bridge.native_runner nor bridge.java_home are set, and JAVA_HOME is empty).\n")
	}
	sb.WriteString("  Remediation: either deploy the native runner next to senhub-agent (recommended) or install a JRE and set bridge.java_home / JAVA_HOME.\n")
	sb.WriteString("  See: senhub-agent ibmi check  ;  docs/admin-guide/IBMI-RUNTIME.md")
	return fmt.Errorf("%s", sb.String())
}
