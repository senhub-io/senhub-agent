package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/services/logger"
)

const (
	dockerSocketPath  = "/var/run/docker.sock"
	systemdRuntimeDir = "/run/systemd/system"
	// defaultsMarkerName records that the defaults were added on this host,
	// so a fragment the operator deletes stays deleted. The package writes
	// the same name in its state directory.
	defaultsMarkerName    = ".default-probes-0.6.4"
	packageDefaultsMarker = "/var/lib/senhub-agent/" + defaultsMarkerName
	// defaultProbesEnv set to "off" keeps the agent from adding default
	// fragments: an image or a fleet whose configuration is managed
	// elsewhere decides its probes itself.
	defaultProbesEnv         = "SENHUB_DEFAULT_PROBES"
	servicesFragmentFile     = "05-services.yaml"
	swarmFragmentFile        = "06-swarm.yaml"
	probeTypeSystemd         = "systemd"
	probeTypeWindowsServices = "winservices"
	probeTypeSwarm           = "swarm"
)

// DefaultFragmentOptions carries the platform and Docker socket checks
// used by EnsureDefaultProbeFragments. It is a test seam; a zero value
// uses runtime.GOOS and os.Stat.
type DefaultFragmentOptions struct {
	GOOS             string
	DockerSocketStat func(string) (os.FileInfo, error)
	// PackageMarker is where the Linux package records that it added the
	// defaults; empty uses the packaged state directory.
	PackageMarker string
	// SystemdRunning reports whether systemd is the init system; a Linux
	// container has no systemd, and the probe would only fail there.
	SystemdRunning func() bool
}

func systemdRunning() bool {
	info, err := os.Stat(systemdRuntimeDir)
	return err == nil && info.IsDir()
}

type defaultFragmentSpec struct {
	fileName  string
	name      string
	probeType string
	content   string
	enabled   bool
}

func defaultFragmentSpecs(opts DefaultFragmentOptions) []defaultFragmentSpec {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	stat := opts.DockerSocketStat
	if stat == nil {
		stat = os.Stat
	}

	running := opts.SystemdRunning
	if running == nil {
		running = systemdRunning
	}

	var specs []defaultFragmentSpec

	switch goos {
	case "linux":
		specs = append(specs, defaultFragmentSpec{
			fileName:  servicesFragmentFile,
			name:      "services",
			probeType: probeTypeSystemd,
			content:   fmt.Sprintf(ServicesProbeFragmentTemplate, probeTypeSystemd),
			enabled:   running(),
		})
		if _, err := stat(dockerSocketPath); err == nil {
			specs = append(specs, defaultFragmentSpec{
				fileName:  swarmFragmentFile,
				name:      "swarm",
				probeType: probeTypeSwarm,
				content:   SwarmProbeFragmentTemplate,
				enabled:   true,
			})
		}
	case "windows":
		specs = append(specs, defaultFragmentSpec{
			fileName:  servicesFragmentFile,
			name:      "services",
			probeType: probeTypeWindowsServices,
			content:   fmt.Sprintf(ServicesProbeFragmentTemplate, probeTypeWindowsServices),
			enabled:   true,
		})
	}

	return specs
}

// EnsureDefaultProbeFragments adds install-default probe fragments that
// a fresh install writes but an existing multi-file install may not have.
// It leaves a legacy monolithic configuration untouched.
func EnsureDefaultProbeFragments(configPath string, log *logger.ModuleLogger, opts DefaultFragmentOptions) {
	if configPath == "" || strings.EqualFold(os.Getenv(defaultProbesEnv), "off") {
		return
	}

	raw, err := os.ReadFile(configPath) // #nosec G304 - operator-provided config path
	if err != nil {
		if log != nil {
			log.Warn().Err(err).Str("path", configPath).Msg("Default probe fragments not checked: cannot read config")
		}
		return
	}
	if HasMonolithicMarkers(raw) {
		return
	}

	probesDir := filepath.Join(filepath.Dir(configPath), "probes.d")
	existingTypes, err := probeTypesConfigured(probesDir)
	if err != nil {
		if log != nil {
			log.Warn().Err(err).Str("path", probesDir).Msg("Default probe fragments not written: cannot read probe fragments")
		}
		return
	}

	marker := filepath.Join(filepath.Dir(configPath), defaultsMarkerName)
	pkgMarker := opts.PackageMarker
	if pkgMarker == "" {
		pkgMarker = packageDefaultsMarker
	}
	if fragmentFileExists(marker) || fragmentFileExists(pkgMarker) {
		return
	}
	defer func() {
		if err := atomicWriteFile(marker, nil, 0600); err != nil && log != nil {
			log.Warn().Err(err).Str("path", marker).Msg("Default probe fragments marker not written; they may be added again at the next start")
		}
	}()

	for _, spec := range defaultFragmentSpecs(opts) {
		if !spec.enabled {
			continue
		}

		path := filepath.Join(probesDir, spec.fileName)
		disabledPath := path + ".disabled"
		if fragmentFileExists(path) || fragmentFileExists(disabledPath) {
			continue
		}
		if existingTypes[spec.probeType] {
			continue
		}

		if err := os.MkdirAll(probesDir, 0750); err != nil {
			if log != nil {
				log.Warn().Err(err).Str("path", probesDir).Msg("Default probe fragment not written: cannot create probes.d")
			}
			continue
		}
		if err := atomicWriteFile(path, []byte(spec.content), fileModeOr(path, 0600)); err != nil {
			if log != nil {
				log.Warn().Err(err).Str("path", path).Msg("Default probe fragment write failed")
			}
			continue
		}
		if log != nil {
			log.Info().
				Str("path", path).
				Str("probe_type", spec.probeType).
				Msg("Default probe fragment written")
		}
	}
}

func probeTypesConfigured(probesDir string) (map[string]bool, error) {
	types := map[string]bool{}

	entries, err := os.ReadDir(probesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return types, nil
		}
		return nil, fmt.Errorf("read %s: %w", probesDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".disabled") {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(probesDir, name)) // #nosec G304 - fragment under probes.d
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		var probes []ProbeConfig
		if err := yaml.Unmarshal(raw, &probes); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for _, p := range probes {
			if p.Type != "" {
				types[p.Type] = true
			}
		}
	}

	return types, nil
}

func fragmentFileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return !os.IsNotExist(err)
}
