package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestAgentConfig(t *testing.T, dir string) string {
	t.Helper()
	configPath := filepath.Join(dir, "agent.yaml")
	probesDir := filepath.Join(dir, "probes.d")
	if err := os.MkdirAll(probesDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("config_version: 2\nagent:\n  key: test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probesDir, "00-host.yaml"), []byte(HostProbesFragmentTemplate), 0600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func testPathExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatalf("stat %s: %v", path, err)
	return false
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	}
}

func TestEnsureDefaultProbeFragments(t *testing.T) {
	socketYes := func(string) (os.FileInfo, error) { return nil, nil }

	t.Run("linux without systemd writes no services probe", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return false },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})
		if _, err := os.Stat(filepath.Join(dir, "probes.d", "05-services.yaml")); !os.IsNotExist(err) {
			t.Fatalf("a host without systemd must not get the systemd probe, stat err=%v", err)
		}
		assertFileContains(t, filepath.Join(dir, "probes.d", "06-swarm.yaml"), "type: swarm")
	})

	t.Run("SENHUB_DEFAULT_PROBES=off writes nothing", func(t *testing.T) {
		t.Setenv("SENHUB_DEFAULT_PROBES", "off")
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})
		for _, name := range []string{"05-services.yaml", "06-swarm.yaml"} {
			if _, err := os.Stat(filepath.Join(dir, "probes.d", name)); !os.IsNotExist(err) {
				t.Errorf("%s written despite the opt-out", name)
			}
		}
	})

	t.Run("a deleted fragment stays deleted", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		opts := DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			PackageMarker:    filepath.Join(dir, "no-package-marker"),
		}
		EnsureDefaultProbeFragments(cfg, nil, opts)
		services := filepath.Join(dir, "probes.d", "05-services.yaml")
		if err := os.Remove(services); err != nil {
			t.Fatal(err)
		}
		EnsureDefaultProbeFragments(cfg, nil, opts)
		if _, err := os.Stat(services); !os.IsNotExist(err) {
			t.Fatalf("a fragment the operator deleted came back, stat err=%v", err)
		}
	})

	t.Run("the package marker means the defaults were already added", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		pkgMarker := filepath.Join(dir, "pkg-marker")
		if err := os.WriteFile(pkgMarker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning: func() bool { return true },
			GOOS:           "linux",
			PackageMarker:  pkgMarker,
		})
		if _, err := os.Stat(filepath.Join(dir, "probes.d", "05-services.yaml")); !os.IsNotExist(err) {
			t.Fatalf("the agent wrote a default the package had already handled, stat err=%v", err)
		}
	})

	t.Run("linux with docker socket writes services and swarm", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})

		servicesPath := filepath.Join(dir, "probes.d", "05-services.yaml")
		swarmPath := filepath.Join(dir, "probes.d", "06-swarm.yaml")
		assertFileContains(t, servicesPath, "name: services")
		assertFileContains(t, servicesPath, "type: systemd")
		assertFileContains(t, swarmPath, "name: swarm")
		assertFileContains(t, swarmPath, "type: swarm")
	})

	t.Run("windows writes winservices and no swarm", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "windows",
			DockerSocketStat: socketYes,
		})

		servicesPath := filepath.Join(dir, "probes.d", "05-services.yaml")
		swarmPath := filepath.Join(dir, "probes.d", "06-swarm.yaml")
		assertFileContains(t, servicesPath, "type: winservices")
		if testPathExists(t, swarmPath) {
			t.Fatalf("%s should not exist on windows", swarmPath)
		}
	})

	t.Run("existing install gets missing fragments", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})

		if !testPathExists(t, filepath.Join(dir, "probes.d", "05-services.yaml")) {
			t.Fatal("05-services.yaml was not written")
		}
		if !testPathExists(t, filepath.Join(dir, "probes.d", "06-swarm.yaml")) {
			t.Fatal("06-swarm.yaml was not written")
		}
	})

	t.Run("disabled twin prevents fragment", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "probes.d", "05-services.yaml.disabled"), []byte(""), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "probes.d", "06-swarm.yaml.disabled"), []byte(""), 0600); err != nil {
			t.Fatal(err)
		}

		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})

		if testPathExists(t, filepath.Join(dir, "probes.d", "05-services.yaml")) {
			t.Fatal("05-services.yaml should not be written when its .disabled twin exists")
		}
		if testPathExists(t, filepath.Join(dir, "probes.d", "06-swarm.yaml")) {
			t.Fatal("06-swarm.yaml should not be written when its .disabled twin exists")
		}
	})

	t.Run("same probe type in another fragment prevents default", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		custom := `- name: custom_services
  type: systemd
  params:
    interval: 120

- name: custom_swarm
  type: swarm
  params:
    interval: 120
`
		if err := os.WriteFile(filepath.Join(dir, "probes.d", "10-custom.yaml"), []byte(custom), 0600); err != nil {
			t.Fatal(err)
		}

		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})

		if testPathExists(t, filepath.Join(dir, "probes.d", "05-services.yaml")) {
			t.Fatal("05-services.yaml should not be written when a systemd probe already exists")
		}
		if testPathExists(t, filepath.Join(dir, "probes.d", "06-swarm.yaml")) {
			t.Fatal("06-swarm.yaml should not be written when a swarm probe already exists")
		}
	})

	t.Run("darwin writes neither", func(t *testing.T) {
		dir := t.TempDir()
		cfg := writeTestAgentConfig(t, dir)
		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "darwin",
			DockerSocketStat: socketYes,
		})

		if testPathExists(t, filepath.Join(dir, "probes.d", "05-services.yaml")) {
			t.Fatal("05-services.yaml should not exist on darwin")
		}
		if testPathExists(t, filepath.Join(dir, "probes.d", "06-swarm.yaml")) {
			t.Fatal("06-swarm.yaml should not exist on darwin")
		}
	})

	t.Run("monolithic layout untouched", func(t *testing.T) {
		dir := t.TempDir()
		cfg := filepath.Join(dir, "agent-config.yaml")
		if err := os.WriteFile(cfg, []byte("probes:\n  - name: cpu\n    type: cpu\nstorage:\n  - name: http\n"), 0600); err != nil {
			t.Fatal(err)
		}

		EnsureDefaultProbeFragments(cfg, nil, DefaultFragmentOptions{
			SystemdRunning:   func() bool { return true },
			GOOS:             "linux",
			DockerSocketStat: socketYes,
		})

		if testPathExists(t, filepath.Join(dir, "probes.d", "05-services.yaml")) {
			t.Fatal("05-services.yaml should not be written for a monolithic config")
		}
		if testPathExists(t, filepath.Join(dir, "probes.d", "06-swarm.yaml")) {
			t.Fatal("06-swarm.yaml should not be written for a monolithic config")
		}
	})
}

func TestIsInstallDefaultFragmentRecognisesNewTemplates(t *testing.T) {
	dir := t.TempDir()
	probesDir := filepath.Join(dir, "probes.d")
	if err := os.MkdirAll(probesDir, 0750); err != nil {
		t.Fatal(err)
	}

	servicesPath := filepath.Join(probesDir, "05-services.yaml")
	if err := os.WriteFile(servicesPath, []byte(fmt.Sprintf(ServicesProbeFragmentTemplate, "systemd")), 0600); err != nil {
		t.Fatal(err)
	}
	if !isInstallDefaultFragment(servicesPath) {
		t.Fatal("05-services.yaml generated from the template should be recognised as an install default")
	}

	swarmPath := filepath.Join(probesDir, "06-swarm.yaml")
	if err := os.WriteFile(swarmPath, []byte(SwarmProbeFragmentTemplate), 0600); err != nil {
		t.Fatal(err)
	}
	if !isInstallDefaultFragment(swarmPath) {
		t.Fatal("06-swarm.yaml generated from the template should be recognised as an install default")
	}
}
