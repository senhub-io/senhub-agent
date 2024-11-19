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
)

type wifiSignalStrengthProbe struct {
	rawConfig map[string]interface{}
	logger    *logger.Logger
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
		m.logger.Error().Msgf("OS not supported")
		return []data_store.DataPoint{}, nil
	}
}

func (m *wifiSignalStrengthProbe) collectWindows() ([]data_store.DataPoint, error) {

	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		return []data_store.DataPoint{}, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Signal") {
			parts := strings.Fields(line)
			if len(parts) > 1 {

				signalStrengthStr := strings.TrimSuffix(parts[len(parts)-1], "%")
				signalStrength, err := strconv.Atoi(signalStrengthStr)
				if err != nil {
					m.logger.Error().Msgf("Error parsing signal strength: %v", err)
					return []data_store.DataPoint{}, err
				}
				return []data_store.DataPoint{
					{Name: "wifi_signal_strength", Timestamp: time.Now(),
						Value: float32(signalStrength)},
				}, nil
			}
		}
	}

	return []data_store.DataPoint{}, nil
}

func (m *wifiSignalStrengthProbe) collectLinux() ([]data_store.DataPoint, error) {
	cmd := exec.Command("iwconfig")
	output, err := cmd.Output()
	if err != nil {
		m.logger.Error().Msgf("Error retrieving Wi-Fi signal strength: %v", err)
		return []data_store.DataPoint{}, err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Signal level=") {
			parts := strings.Fields(line)
			for _, part := range parts {
				if strings.HasPrefix(part, "Signal level=") {
					signal := strings.Split(part, "=")[1]
					signalStrength, _ := strconv.Atoi(signal)
					return []data_store.DataPoint{
						{Name: "wifi_signal_strength", Timestamp: time.Now(),
							Value: float32(signalStrength)},
					}, nil
				}
			}
		}
	}
	return []data_store.DataPoint{}, nil
}

func (m *wifiSignalStrengthProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}
func (m *wifiSignalStrengthProbe) OnShutdown(ctx context.Context) error {
	return nil
}
