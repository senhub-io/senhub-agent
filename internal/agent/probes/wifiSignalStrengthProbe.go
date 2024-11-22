package probes

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

type wifiSignalStrengthProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
}

func (m *wifiSignalStrengthProbe) checkWifiWindows() bool {
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		m.logger.Error().Msgf("Error checking WiFi connection: %v", err)
		return false
	}

	// Check if a WiFi connection is active
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "State") {
			// Check for both English and French status as Windows can be localized
			return strings.Contains(strings.ToLower(line), "connected") ||
				strings.Contains(strings.ToLower(line), "connecté")
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
		m.logger.Error().Msgf("Error checking WiFi connection: %v", err)
		return false
	}

	return strings.Contains(strings.ToLower(string(output)), "enabled")
}

func NewWifiSignalStrengthProbe(config map[string]interface{}, logger *logger.Logger) (Probe, error) {
	// No validation needed for this probe
	return &wifiSignalStrengthProbe{
		rawConfig: config,
		logger:    logger,
	}, nil
}

func (m *wifiSignalStrengthProbe) GetName() string {
	return "WifiSignalStrengthProbe"
}

func (m *wifiSignalStrengthProbe) ShouldStart() bool {
	return true
}

func (m *wifiSignalStrengthProbe) GetInterval() time.Duration {
	return 2 * time.Second
}

func (m *wifiSignalStrengthProbe) Collect() ([]data_store.DataPoint, error) {

	switch runtime.GOOS {
	case "windows":
		return m.collectWindows()
	case "linux":
		return m.collectLinux()
	default:
		m.logger.Warn().Msgf("OS not supported")
		return []data_store.DataPoint{}, nil
	}
}

func (m *wifiSignalStrengthProbe) collectWindows() ([]data_store.DataPoint, error) {
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		return []data_store.DataPoint{}, err
	}

	var signalStrength int
	var bssid string

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Get signal strength
		if strings.Contains(line, "Signal") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				signalStrengthStr := strings.TrimSuffix(parts[len(parts)-1], "%")
				signalStrength, err = strconv.Atoi(signalStrengthStr)
				if err != nil {
					m.logger.Error().Msgf("Error parsing signal strength: %v", err)
					continue
				}
			}
		}
		// Get BSSID (MAC address of the access point)
		if strings.Contains(line, "BSSID") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				bssid = parts[len(parts)-1]
			}
		}
	}

	if signalStrength == 0 || bssid == "" {
		return []data_store.DataPoint{}, nil
	}

	// Create tags slice with both PRTG metric ID and BSSID
	tags := []tags.Tag{
		data_store.CreatePrtgMetricIdTag("[name]"),
		{Key: "bssid", Value: bssid},
	}

	return []data_store.DataPoint{
		{
			Name:      "wifi_signal_strength",
			Timestamp: time.Now(),
			Value:     float32(signalStrength),
			Tags:      tags,
		},
	}, nil
}

func (m *wifiSignalStrengthProbe) collectLinux() ([]data_store.DataPoint, error) {
	cmd := exec.Command("iwconfig")
	output, err := cmd.Output()
	if err != nil {
		m.logger.Error().Msgf("Error retrieving Wi-Fi information: %v", err)
		return []data_store.DataPoint{}, err
	}

	var dataPoints []data_store.DataPoint
	var bssid string
	timestamp := time.Now()

	// First pass to get BSSID
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Access Point:") {
			bssid = strings.TrimSpace(strings.Split(line, "Access Point:")[1])
			if bssid == "Not-Associated" {
				return []data_store.DataPoint{}, nil
			}
			break
		}
	}

	// Create base tags with BSSID
	tags := []tags.Tag{
		{Key: "bssid", Value: bssid},
	}

	// Second pass for metrics
	for _, line := range lines {
		if strings.Contains(line, "Signal level=") {
			// Get signal level
			signalMatch := strings.Split(strings.Split(line, "Signal level=")[1], " ")[0]
			signalStrength, err := strconv.Atoi(strings.TrimSpace(signalMatch))
			if err == nil {
				dataPoints = append(dataPoints, data_store.DataPoint{
					Name:      "wifi_signal_strength",
					Timestamp: timestamp,
					Value:     float32(signalStrength),
					Tags:      tags,
				})
			}

			// Get Link Quality if available
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
							Tags:      tags,
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
