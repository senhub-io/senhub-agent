package probes

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

type wifiSignalStrengthProbe struct {
}

func NewWifiSignalStrengthProbe() Probe {
	return &wifiSignalStrengthProbe{}
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
	var s runtime.MemStats
	runtime.ReadMemStats(&s)
	switch runtime.GOOS {
	case "windows":
		return m.collectWindows()
	case "linux":
		return m.collectLinux()
	default:
		log.Println("OS not supported")
		return []data_store.DataPoint{}, nil
	}
}
func (m *wifiSignalStrengthProbe) collectWindows() ([]data_store.DataPoint, error) {
	// Exécuter la commande `netsh wlan show interfaces` pour récupérer les infos Wi-Fi
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	output, err := cmd.Output()
	if err != nil {
		return []data_store.DataPoint{}, err
	}

	// Analyser la sortie pour trouver la ligne contenant "Signal"
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Signal") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				// Extraire le pourcentage du signal et enlever le symbole "%"
				signalStrengthStr := strings.TrimSuffix(parts[len(parts)-1], "%")
				signalStrength, err := strconv.Atoi(signalStrengthStr)
				if err != nil {
					log.Println("Error parsing signal strength:", err)
					return []data_store.DataPoint{}, err
				}
				return []data_store.DataPoint{
					{Name: "wifi_signal_strength", Value: fmt.Sprintf("%d", signalStrength)},
				}, nil
			}
		}
	}

	// Retourne 0 si la force du signal n'est pas trouvée
	return []data_store.DataPoint{}, nil
}
func (m *wifiSignalStrengthProbe) collectLinux() ([]data_store.DataPoint, error) {
	cmd := exec.Command("iwconfig")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error retrieving Wi-Fi signal strength:", err)
		return []data_store.DataPoint{}, err
	}

	// Parse the output to find signal strength
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Signal level=") {
			parts := strings.Fields(line)
			for _, part := range parts {
				if strings.HasPrefix(part, "Signal level=") {
					signal := strings.Split(part, "=")[1]
					signalStrength, _ := strconv.Atoi(signal)
					return []data_store.DataPoint{
						{Name: "wifi_signal_strength", Value: fmt.Sprintf("%d", signalStrength)},
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
