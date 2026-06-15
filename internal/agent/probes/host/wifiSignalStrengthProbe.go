package host

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type wifiSignalStrengthProbe struct {
	*types.BaseProbe // Ajout de BaseProbe
	rawConfig        map[string]interface{}
	moduleLogger     *logger.ModuleLogger
}

func (m *wifiSignalStrengthProbe) checkWifiWindows() bool {
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		m.moduleLogger.Error().Err(err).Msg("Error checking WiFi connection")
		return false
	}

	outputStr := string(output)

	// On cherche des patterns simples qui marchent même avec des problèmes d'encodage
	// Ceci est un test
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		line = strings.ToLower(line)

		// On cherche les indicateurs d'état (état ou state)
		if strings.Contains(line, "tat") || strings.Contains(line, "state") {
			// Si on trouve "connect" sans "non" ou "dis" avant, c'est connecté
			if strings.Contains(line, "connect") {
				if strings.Contains(line, "non") || strings.Contains(line, "dis") {
					return false
				}
				return true
			}
		}
	}
	return false
}

func (m *wifiSignalStrengthProbe) checkWifiLinux() bool {
	// First attempt using iwconfig
	cmd := exec.Command("iwconfig")
	output, err := cmd.Output()
	if err == nil {
		if strings.Contains(string(output), "ESSID:") &&
			!strings.Contains(string(output), "ESSID:off/any") {
			return true
		}
	}
	// Second attempt using nmcli if iwconfig fails
	cmd = exec.Command("nmcli", "-t", "-f", "WIFI", "radio")
	output, err = cmd.Output()
	if err != nil {
		m.moduleLogger.Error().Err(err).Msg("Error checking WiFi connection")
		return false
	}
	return strings.Contains(strings.ToLower(string(output)), "enabled")
}

func NewWifiSignalStrengthProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	// Create module-specific logger for wifi probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.wifi")

	return &wifiSignalStrengthProbe{
		BaseProbe:    &types.BaseProbe{},
		rawConfig:    config,
		moduleLogger: moduleLogger,
	}, nil
}

func (p *wifiSignalStrengthProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "wifi_signal_strength", "wifi2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

func (m *wifiSignalStrengthProbe) ShouldStart() bool {
	switch runtime.GOOS {
	case "windows":
		return m.checkWifiWindows()
	case "linux":
		return m.checkWifiLinux()
	default:
		m.moduleLogger.Error().Str("os", runtime.GOOS).Msg("Unsupported operating system")
		return false
	}
}

func (m *wifiSignalStrengthProbe) GetInterval() time.Duration {
	return 60 * time.Second
}

func (m *wifiSignalStrengthProbe) Collect() ([]data_store.DataPoint, error) {
	var metrics []data_store.DataPoint
	var err error

	switch runtime.GOOS {
	case "windows":
		metrics, err = m.collectWindows()
	case "linux":
		metrics, err = m.collectLinux()
	default:
		err = fmt.Errorf("OS not supported")
		return []data_store.DataPoint{}, err
	}

	if err != nil {
		return nil, err
	}

	return m.finish(metrics), nil
}

// finish enriches collected datapoints unconditionally, like every
// other host probe: the enrichment used to live inside the dead
// OnDataPoints branch (SetOnDataPoints has zero callers), so wifi
// datapoints reached the store without probe_name/probe_type —
// skipping the transformer, defeating per-probe custom_tags and
// mispartitioning OTLP series under the empty probe key (#264).
func (m *wifiSignalStrengthProbe) finish(metrics []data_store.DataPoint) []data_store.DataPoint {
	return m.BaseProbe.EnrichDataPointsWithProbeName(metrics, m.GetName())
}

func (m *wifiSignalStrengthProbe) collectWindows() ([]data_store.DataPoint, error) {
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute netsh command: %v", err)
	}

	var signalStrength int
	var ssid, bssid string
	foundSignal := false

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(line), "signal") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				signalStrengthStr := strings.TrimSuffix(parts[len(parts)-1], "%")
				signalStrength, err = strconv.Atoi(signalStrengthStr)
				if err != nil {
					return nil, fmt.Errorf("error parsing signal strength: %v", err)
				}
				foundSignal = true
			}
		}
		// Gestion française et anglaise du SSID
		if (strings.HasPrefix(line, "SSID") && !strings.Contains(line, "identificateur")) ||
			(strings.Contains(line, "SSID") && !strings.Contains(line, "BSSID") && !strings.Contains(line, "identificateur")) {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				ssid = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "BSSID") || strings.Contains(line, "Point d'accès d'identificateur SSID") {
			// Find the index of the first ":" character
			colonIndex := strings.Index(line, ":")
			if colonIndex != -1 {
				// Extract everything after the first ":" (to preserve BSSID format xx:xx:xx:xx:xx:xx)
				bssid = strings.TrimSpace(line[colonIndex+1:])
			}
		}
	}

	if !foundSignal {
		return nil, fmt.Errorf("could not find WiFi signal strength")
	}

	var wifiTags []tags.Tag
	if ssid != "" {
		wifiTags = append(wifiTags, tags.Tag{Key: "ssid", Value: ssid, Private: false})
	}
	if bssid != "" {
		wifiTags = append(wifiTags, tags.Tag{Key: "bssid", Value: bssid, Private: false})
	}

	return []data_store.DataPoint{
		{
			Name:      "wifi_signal_strength",
			Timestamp: time.Now(),
			Value:     float32(signalStrength),
			Tags:      wifiTags,
		},
	}, nil
}

func (m *wifiSignalStrengthProbe) collectLinux() ([]data_store.DataPoint, error) {
	cmd := exec.Command("iwconfig")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error retrieving Wi-Fi information: %v", err)
	}

	var dataPoints []data_store.DataPoint
	var ssid, bssid string
	timestamp := time.Now()

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Access Point:") {
			bssid = strings.TrimSpace(strings.Split(line, "Access Point:")[1])
			if bssid == "Not-Associated" {
				return nil, nil
			}
		}
		if strings.Contains(line, "ESSID:") {
			ssid = strings.Trim(strings.Split(line, "ESSID:")[1], "\"")
		}
	}

	var wifiTags []tags.Tag
	if bssid != "" {
		wifiTags = append(wifiTags, tags.Tag{Key: "bssid", Value: bssid, Private: false})
	}
	if ssid != "" {
		wifiTags = append(wifiTags, tags.Tag{Key: "ssid", Value: ssid, Private: false})
	}

	for _, line := range lines {
		if strings.Contains(line, "Signal level=") {
			signalMatch := strings.Split(strings.Split(line, "Signal level=")[1], " ")[0]
			signalStrength, err := strconv.Atoi(strings.TrimSpace(signalMatch))
			if err == nil {
				dataPoints = append(dataPoints, data_store.DataPoint{
					Name:      "wifi_signal_strength",
					Timestamp: timestamp,
					Value:     float32(signalStrength),
					Tags:      wifiTags,
				})
			}

			if strings.Contains(line, "Link Quality=") {
				qualityStr := strings.Split(strings.Split(line, "Link Quality=")[1], " ")[0]
				qualityParts := strings.Split(qualityStr, "/")
				if len(qualityParts) == 2 {
					quality, err := strconv.Atoi(qualityParts[0])
					maxQuality, err2 := strconv.Atoi(qualityParts[1])
					if err == nil && err2 == nil && maxQuality > 0 {
						qualityPercent := float32(quality) / float32(maxQuality) * 100
						dataPoints = append(dataPoints, data_store.DataPoint{
							Name:      "wifi_quality",
							Timestamp: timestamp,
							Value:     qualityPercent,
							Tags:      wifiTags,
						})
					}
				}
			}
		}
	}
	return dataPoints, nil
}

func (m *wifiSignalStrengthProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}
func (m *wifiSignalStrengthProbe) OnShutdown(ctx context.Context) error {
	return nil
}
