package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"math"

	"github.com/getlantern/systray"
	"github.com/ncruces/zenity"
)

// Struct to hold the configuration response
type ConfigResponse struct {
	URL string `json:"url"` // Adjust this field according to the actual JSON structure
}

// Struct to hold metrics for POST request
type Metrics struct {
	GatewayLoss          int     `json:"gateway_loss"`
	GatewayLatency       int     `json:"gateway_latency"`
	MonitoredLoss        int     `json:"monitored_loss"`
	MonitoredLatency     int     `json:"monitored_latency"`
	DomLoadTime          int     `json:"dom_load_time"`
	PageLoadTime         int     `json:"page_load_time"`
	WifiSignalStrength    int     `json:"wifi_signal_strength"`
	Timestamp            string  `json:"timestamp"`
}

var apiKey string

func onReady() {
	// Load an icon for the systray
	iconData, err := ioutil.ReadFile("icon.png")
	if err != nil {
		log.Fatal(err)
	}
	systray.SetIcon(iconData)
	systray.SetTitle("Network Monitor")
	systray.SetTooltip("Network Monitoring Agent")

	// Add a menu item to set the API key
	mSetAPIKey := systray.AddMenuItem("Set API Key", "Enter the API Key")
	mStart := systray.AddMenuItem("Start Monitoring", "Start Network Monitoring")
	mQuit := systray.AddMenuItem("Quit", "Quit the application")

	go func() {
		for {
			select {
			case <-mSetAPIKey.ClickedCh:
				// Open a dialog to set the API key
				key, err := zenity.Entry("Enter your API Key", zenity.Title("API Key"))
				if err != nil {
					if err == zenity.ErrCanceled {
						log.Println("API Key input canceled")
					} else {
						log.Println("Error reading API Key:", err)
					}
				} else {
					apiKey = key
					log.Printf("API Key set: %s\n", apiKey)
				}
			case <-mStart.ClickedCh:
				if apiKey == "" {
					zenity.Error("Please set an API Key before starting the monitoring.")
				} else {
					go startMonitoring()
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	log.Println("Exiting application")
}


// Function to get the local gateway IP address
func getGatewayIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
					gateway := net.ParseIP(ipnet.IP.String())
					return gateway.String(), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no active gateway found")
}

// Function to ping an IP address and return metrics
func ping(ip string) (int, int, error) {
	var cmd *exec.Cmd
	count := 10 // Number of packets to send

	// Determine the OS and set command accordingly
	switch runtime.GOOS {
	case "linux", "darwin": // macOS is treated the same as Linux for ping
		cmd = exec.Command("ping", "-c", strconv.Itoa(count), ip)
	case "windows":
		cmd = exec.Command("ping", "-n", strconv.Itoa(count), ip)
	default:
		return 0, 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ping command failed: %v, output: %s", err, string(output))
	}

	outputStr := string(output)
	var totalLatency float64
	var packetLoss int

	// Adjust this logic based on the expected output format for different OS
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
    if strings.Contains(line, "avg") || strings.Contains(line, "Average") {
    // Split the string by slashes to separate min/avg/max/stddev
    parts := strings.Fields(line)
    if len(parts) >= 5 {
        // Extract the second member, which is the average latency
        latency := strings.Split(parts[3], "/")[1]
        fmt.Printf("Latency: %s\n", latency)
        latencyFloat, err := strconv.ParseFloat(latency, 64)
        if err == nil {
            totalLatency += latencyFloat // Add the average latency to the total
        }
      }
    }

		if strings.Contains(line, "packet loss") || strings.Contains(line, "Lost") {
			parts := strings.Fields(line)
			if len(parts) > 6 {
				packetLoss, _ = strconv.Atoi(parts[5])
			}
		}
	}

	if count > 0 {
		// Round the average latency to the nearest integer
		averageLatency := int(math.Round(totalLatency / float64(count)))
		return averageLatency, packetLoss, nil
	}
	return 0, packetLoss, nil
}

// Function to get the IP address from a URL
func getIPFromURL(pageURL string) (string, error) {
	ips, err := net.LookupIP(pageURL)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		return ip.String(), nil
	}
	return "", fmt.Errorf("no IP address found for URL: %s", pageURL)
}

// Function to measure page load times
func measurePageLoad(pageURL string) (int, int, error) {
	// Parse the URL to ensure it has the correct format
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return 0, 0, err
	}

	// Ensure the URL has the http or https scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return 0, 0, fmt.Errorf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
	}

	start := time.Now()
	resp, err := http.Get(pageURL)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	// Simulate DOM load time as half of the total time taken to fetch the page
	fullLoadTime := time.Since(start).Seconds() * 1000 // Convert to milliseconds
	domLoadTime := fullLoadTime / 2                     // Estimate DOM load time

	// Round both times to the nearest integer
	return int(math.Round(domLoadTime)), int(math.Round(fullLoadTime)), nil
}

// Function to get the URL from the specified endpoint
func getURLFromEndpoint(apiKey string) (string, error) {
	endpoint := fmt.Sprintf("https://nats.sensorfactory.eu:8443/configs?api_key=%s", apiKey)

	// Debugging: print the generated endpoint
	fmt.Printf("Generated Endpoint: %s\n", endpoint)

	// Create a new request
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	// Add the x-api-key header
	req.Header.Set("x-api-key", apiKey)

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error: received status code %d from endpoint", resp.StatusCode)
	}

	// Decode the JSON response
	var configResponse ConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&configResponse); err != nil {
		return "", err
	}

	return configResponse.URL, nil
}

// Function to get the local Wi-Fi signal strength based on the OS
func getWifiSignalStrength() int {
	var cmd *exec.Cmd
	var output []byte
	var err error

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("iwconfig")
		output, err = cmd.Output()
		if err != nil {
			fmt.Println("Error retrieving Wi-Fi signal strength:", err)
			return 0 // Return 0 if there's an error
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
						return signalStrength
					}
				}
			}
		}
	case "windows":
		cmd = exec.Command("netsh", "wlan", "show", "interfaces")
		output, err = cmd.Output()
		if err != nil {
			fmt.Println("Error retrieving Wi-Fi signal strength:", err)
			return 0 // Return 0 if there's an error
		}

		// Parse the output to find signal strength
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Signal") {
				parts := strings.Fields(line)
				signalStrength, _ := strconv.Atoi(parts[1][:len(parts[1])-1]) // Remove the % sign
				return signalStrength
			}
		}
	case "darwin": // macOS
		cmd = exec.Command("/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport", "-I")
		output, err = cmd.Output()
		if err != nil {
			fmt.Println("Error retrieving Wi-Fi signal strength:", err)
			return 0 // Return 0 if there's an error
		}

		// Parse the output to find signal strength
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "agrCtlRSSI") {
				parts := strings.Fields(line)
				signalStrength, _ := strconv.Atoi(parts[1]) // agrCtlRSSI gives signal strength directly
				return signalStrength
			}
		}
	default:
		fmt.Printf("unsupported OS: %s\n", runtime.GOOS)
		return 0 // Return 0 if the OS is unsupported
	}

	return 0 // Return 0 if the signal strength cannot be determined
}

// Function to POST metrics to the specified endpoint
func postMetrics(apiKey string, metrics Metrics) error {
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return err
	}

	// Specify the metrics endpoint
	endpoint := "https://nats.sensorfactory.eu:8443/metrics"

	// Create a new request
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(metricsJSON))
	if err != nil {
		return err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error: received status code %d from metrics endpoint", resp.StatusCode)
	}

	return nil
}

func startMonitoring() {
	for {
		// Ensure the API key is set
		if apiKey == "" {
			fmt.Println("API key not set, cannot start monitoring.")
			return
		}

		// Get the URL from the specified endpoint
		pageURL, err := getURLFromEndpoint(apiKey)
		if err != nil {
			fmt.Println("Error retrieving URL from endpoint:", err)
			return
		}

		fmt.Println("Retrieved URL:", pageURL)

		// Get the gateway IP address
		gatewayIP, err := getGatewayIP()
		if err != nil {
			fmt.Println("Error retrieving gateway IP address:", err)
			return
		}

		fmt.Println("Gateway IP address:", gatewayIP)

		// Ping the gateway and get metrics
		gatewayLatency, gatewayPacketLoss, err := ping(gatewayIP)
		if err != nil {
			fmt.Println("Error pinging the gateway:", err)
			return
		}

		fmt.Printf("Average Latency for Gateway: %d ms\n", gatewayLatency)
		fmt.Printf("Packet Loss for Gateway: %d\n", gatewayPacketLoss)

		// Get the Wi-Fi signal strength
		wifiSignalStrength := getWifiSignalStrength()

		// Get the hostname from the retrieved URL for pinging
		parsedURL, err := url.Parse(pageURL)
		if err != nil {
			fmt.Println("Error parsing URL:", err)
			return
		}

		// Get the IP address from the hostname (without the scheme)
		targetIP, err := getIPFromURL(parsedURL.Hostname())
		if err != nil {
			fmt.Println("Error retrieving IP address from URL:", err)
			return
		}

		fmt.Println("Target IP address:", targetIP)

		// Ping the target and get metrics
		latency, packetLoss, err := ping(targetIP)
		if err != nil {
			fmt.Println("Error pinging the target:", err)
			return
		}

		fmt.Printf("Average Latency for %s: %d ms\n", pageURL, latency)
		fmt.Printf("Packet Loss for %s: %d\n", pageURL, packetLoss)

		// Measure page load times
		domLoadTime, fullLoadTime, err := measurePageLoad(pageURL)
		if err != nil {
			fmt.Println("Error measuring page load times:", err)
			return
		}

		// Get the current timestamp in RFC3339 format
		timestamp := time.Now().Format(time.RFC3339)

		// Prepare metrics for posting
		metrics := Metrics{
			GatewayLoss:        gatewayPacketLoss,
			GatewayLatency:     gatewayLatency,
			MonitoredLoss:      packetLoss,
			MonitoredLatency:   latency,
			DomLoadTime:        domLoadTime,
			PageLoadTime:       fullLoadTime,
			WifiSignalStrength: wifiSignalStrength,
			Timestamp:          timestamp,
		}

		// Post metrics
		if err := postMetrics(apiKey, metrics); err != nil {
			fmt.Println("Error posting metrics:", err)
			return
		}

		// Output the results including the timestamp
		fmt.Printf("Timestamp of Measurement: %s\n", timestamp)
		fmt.Printf("Estimated DOM Load Time for %s: %d ms\n", pageURL, domLoadTime)
		fmt.Printf("Full Page Load Time for %s: %d ms\n", pageURL, fullLoadTime)

		// Wait for 5 minutes before the next iteration
		time.Sleep(5 * time.Minute)
	}
}

func main() {
	if runtime.GOOS == "windows" {
		// Start the systray only if the OS is Windows
		systray.Run(onReady, onExit)
	} else {
		// If not on Windows, just start monitoring directly
		// If not on Windows, require the API key as a command-line argument
		if len(os.Args) < 2 {
			fmt.Println("Usage: ./your_program_name <API_KEY>")
			return
		}
		apiKey = os.Args[1]
		fmt.Printf("Running monitoring with API key: %s\n", apiKey)
		startMonitoring()
	}
}
