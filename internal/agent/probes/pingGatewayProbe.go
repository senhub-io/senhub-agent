package probes

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
)

type PingGatewayProbe struct {
	config map[string]interface{}
	logger *logger.Logger
}

func NewPingGatewayProbe(config map[string]interface{}, logger *logger.Logger) Probe {
    return &PingGatewayProbe{
        config: config,
        logger: logger,
    }
}

func (p *PingGatewayProbe) GetName() string {
	return "pingGatewayProbe"
}

func (p *PingGatewayProbe) ShouldStart() bool {
	return true
}

func (p *PingGatewayProbe) ValidateConfig(config map[string]interface{}) bool {
	return true
}

func (p *PingGatewayProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (p *PingGatewayProbe) Collect() ([]data_store.DataPoint, error) {
	gatewayIP, err := p.getGatewayIP()
	if err != nil {
		return nil, fmt.Errorf("error retrieving gateway IP address: %w", err)
	}

	averageLatency, packetLoss, err := p.collectPing(gatewayIP)
	if err != nil {
		return nil, fmt.Errorf("error collecting ping data: %w", err)
	}

	tags := map[string]string{
			"probe_type": "gateway",
	}

	return []data_store.DataPoint{
		{Name: "averageLatency", Timestamp: time.Now(), Value: float32(averageLatency), Tags: tags},
		{Name: "packetLoss", Timestamp: time.Now(), Value: float32(packetLoss), Tags: tags},
	}, nil
}

func (p *PingGatewayProbe) getGatewayIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
					return ipnet.IP.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("no default gateway found")
}

func (p *PingGatewayProbe) collectPing(ip string) (float32, float32, error) {
	switch runtime.GOOS {
	case "windows":
		return p.collectPingWindows(ip)
	case "linux":
		return p.collectPingLinux(ip)
	case "darwin":
		return p.collectPingDarwin(ip)

	default:
		return 0, 0, fmt.Errorf("OS not supported")
	}
}

func (p *PingGatewayProbe) collectPingWindows(ip string) (float32, float32, error) {
	count := 10
	cmd := exec.Command("ping", "-n", strconv.Itoa(count), ip)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	averageLatency, packetLoss, err := parsePingWindowsOutput(outputStr)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing ping output: %w", err)
	}

	return averageLatency, packetLoss, nil
}

func (p *PingGatewayProbe) collectPingLinux(ip string) (float32, float32, error) {
	count := 10
	cmd := exec.Command("ping", "-c", strconv.Itoa(count), ip)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	averageLatency, packetLoss, err := parsePingLinuxOutput(outputStr)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing ping output: %w", err)
	}

	return averageLatency, packetLoss, nil
}

func (p *PingGatewayProbe) collectPingDarwin(ip string) (float32, float32, error) {
	count := 10
	cmd := exec.Command("ping", "-c", strconv.Itoa(count), ip)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	averageLatency, packetLoss, err := parsePingDarwinOutput(outputStr)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing ping output: %w", err)
	}

	return averageLatency, packetLoss, nil
}

func parsePingWindowsOutput(output string) (float32, float32, error) {
	var averageLatency float32
	var packetLoss float32

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Lost =") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "Lost" && i+2 < len(parts) {
					lossString := strings.Trim(parts[i+2], "(%)")
					lossValue, err := strconv.ParseFloat(lossString, 32)
					if err != nil {
						return 0, 0, fmt.Errorf("error parsing packet loss: %w", err)
					}
					packetLoss = float32(lossValue) // Conversion explicite en float32
					break
				}
			}
		}

		if strings.Contains(line, "Average =") {
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "Average" && i+2 < len(parts) {
					latencyString := strings.TrimSuffix(parts[i+2], "ms")
					latencyValue, err := strconv.ParseFloat(latencyString, 32)
					if err != nil {
						return 0, 0, fmt.Errorf("error parsing average latency: %w", err)
					}
					averageLatency = float32(latencyValue) // Conversion explicite en float32
					break
				}
			}
		}
	}

	return averageLatency, packetLoss, nil
}

func parsePingLinuxOutput(output string) (float32, float32, error) {
	var averageLatency float32
	var packetLoss float32

	// Extract the average ping time from the last line
	rttRegex := regexp.MustCompile(`rtt min/avg/max/mdev = (\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)`)
	rttMatch := rttRegex.FindStringSubmatch(output)

	if len(rttMatch) < 2 {
		return 0, 0, fmt.Errorf("could not find average ping time in output")
	}

	// Convert to float64 and then to float32
	latencyValue, err := strconv.ParseFloat(rttMatch[2], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing average latency: %w", err)
	}
	averageLatency = float32(latencyValue)

	// Extract the packet loss percentage
	lossRegex := regexp.MustCompile(`(\d+\.\d+|\d+)\% packet loss`)
	lossMatch := lossRegex.FindStringSubmatch(output)
	if len(lossMatch) < 2 {
		return 0, 0, fmt.Errorf("could not find packet loss information in output")
	}

	// Convert to float64 and then to float32
	lossValue, err := strconv.ParseFloat(lossMatch[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing packet loss: %w", err)
	}
	packetLoss = float32(lossValue)

	return averageLatency, packetLoss, nil
}

func parsePingDarwinOutput(output string) (float32, float32, error) {
	var averageLatency float32
	var packetLoss float32

	// Extract the average ping time from the last line
	rttRegex := regexp.MustCompile(`round-trip min/avg/max/stddev = (\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)/(\d+\.\d+)`)
	rttMatch := rttRegex.FindStringSubmatch(output)

	if len(rttMatch) < 2 {
		return 0, 0, fmt.Errorf("could not find average ping time in output")
	}

	// Convert to float64 and then to float32
	latencyValue, err := strconv.ParseFloat(rttMatch[2], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing average latency: %w", err)
	}
	averageLatency = float32(latencyValue)

	// Extract the packet loss percentage
	lossRegex := regexp.MustCompile(`(\d+\.\d+|\d+)\% packet loss`)
	lossMatch := lossRegex.FindStringSubmatch(output)
	if len(lossMatch) < 2 {
		return 0, 0, fmt.Errorf("could not find packet loss information in output")
	}

	// Convert to float64 and then to float32
	lossValue, err := strconv.ParseFloat(lossMatch[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing packet loss: %w", err)
	}
	packetLoss = float32(lossValue)

	return averageLatency, packetLoss, nil
}

func (p *PingGatewayProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *PingGatewayProbe) OnShutdown(ctx context.Context) error {
	return nil
}
