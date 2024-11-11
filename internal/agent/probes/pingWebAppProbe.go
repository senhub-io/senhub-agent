package probes

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
)

type PingWebAppProbe struct {
	config map[string]interface{}
}

func NewPingWebAppProbe(config map[string]interface{}) Probe {
	return &PingWebAppProbe{
		config: config,
	}
}

func (p *PingWebAppProbe) GetName() string {
	return "ping_webapp"
}

func (p *PingWebAppProbe) ShouldStart() bool {
	return true
}

func (p *PingWebAppProbe) ValidateConfig(config map[string]interface{}) bool {
	if config["url"] == nil || !config["url"].(bool) {
		log.Printf("url parameter is required for %s probe", p.GetName())
		return false
	}
	return true
}

func (p *PingWebAppProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (p *PingWebAppProbe) Collect() ([]data_store.DataPoint, error) {
	webappURL := p.config["url"].(string)

	webappIP, err := p.resolveHostname(webappURL)
	if err != nil {
		return nil, fmt.Errorf("error retrieving web app IP address: %w", err)
	}

	averageLatency, packetLoss, err := p.collectPing(webappIP)
	if err != nil {
		return nil, fmt.Errorf("error collecting ping data: %w", err)
	}

	return []data_store.DataPoint{
		{Name: "webApp_averageLatency", Timestamp: time.Now(), Value: float32(averageLatency)},
		{Name: "webApp_packetLoss", Timestamp: time.Now(), Value: float32(packetLoss)},
	}, nil
}

func (p *PingWebAppProbe) resolveHostname(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("no hostname found in URL")
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return "", fmt.Errorf("failed to resolve hostname: %w", err)
	}

	if len(ips) > 0 {
		return ips[0].String(), nil
	}

	return "", fmt.Errorf("no IP address found for hostname")
}

func (p *PingWebAppProbe) collectPing(ip string) (float32, float32, error) {
	switch runtime.GOOS {
	case "windows":
		return p.collectPingWebAppWindows(ip)
	case "linux":
		return p.collectPingWebAppLinux(ip)
	case "darwin":
		return p.collectPingWebAppDarwin(ip)

	default:
		return 0, 0, fmt.Errorf("OS not supported")
	}
}

func (p *PingWebAppProbe) collectPingWebAppWindows(ip string) (float32, float32, error) {
	count := 10
	cmd := exec.Command("ping", "-n", strconv.Itoa(count), ip)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	averageLatency, packetLoss, err := parsePingWebAppWindowsOutput(outputStr)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing ping output: %w", err)
	}

	return averageLatency, packetLoss, nil
}

func (p *PingWebAppProbe) collectPingWebAppLinux(ip string) (float32, float32, error) {
	count := 10
	cmd := exec.Command("ping", "-c", strconv.Itoa(count), ip)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	averageLatency, packetLoss, err := parsePingWebAppLinuxOutput(outputStr)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing ping output: %w", err)
	}

	return averageLatency, packetLoss, nil
}

func (p *PingWebAppProbe) collectPingWebAppDarwin(ip string) (float32, float32, error) {
	count := 10
	cmd := exec.Command("ping", "-c", strconv.Itoa(count), ip)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	averageLatency, packetLoss, err := parsePingWebAppDarwinOutput(outputStr)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing ping output: %w", err)
	}
	return averageLatency, packetLoss, nil
}

func parsePingWebAppWindowsOutput(output string) (float32, float32, error) {
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

func parsePingWebAppLinuxOutput(output string) (float32, float32, error) {
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

func parsePingWebAppDarwinOutput(output string) (float32, float32, error) {
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

func (p *PingWebAppProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *PingWebAppProbe) OnShutdown(ctx context.Context) error {
	return nil
}
