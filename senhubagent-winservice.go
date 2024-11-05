package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows/svc"
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

type Config struct {
	AgentKey string `json:"agent_key"`
	EnableLogging bool   `json:"enable_logging"`
}

type myService struct{}

var (
	configFilePath = "config.json" // Le chemin où la config sera sauvegardée
	AgentKey         string
	enableLogging  bool
	monitoring     bool        // Indicateur pour savoir si le monitoring est actif
	stopChan       chan bool   // Canal pour arrêter le monitoring
	logger         *log.Logger
)

func (m *myService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
    // Indiquer que le service est en cours de démarrage
    s <- svc.Status{State: svc.StartPending}

    // Lancer le monitoring dans une routine pour que le service démarre rapidement
    go func() {
        s <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
        startMonitoring() // Votre fonction de démarrage du monitoring
    }()

    // Gestion des commandes de contrôle du service (arrêt, interrogation, etc.)
    for {
        select {
        case c := <-r:
            switch c.Cmd {
            case svc.Interrogate:
                s <- c.CurrentStatus
            case svc.Stop, svc.Shutdown:
                s <- svc.Status{State: svc.StopPending}
                stopMonitoring() // Arrêter le monitoring proprement
                return true, 0
            }
        }
    }
}

func runService(name string, handler svc.Handler) error {
    return svc.Run(name, handler)
}

// Initialiser le logger
func initLogger() {
	if enableLogging {
		file, err := os.OpenFile("agent.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logger.Fatalf("Failed to open log file: %v", err)
		}
		logger = log.New(file, "", log.LstdFlags)
	} else {
		// Si le log est désactivé, le logger ne fera rien
		logger = log.New(ioutil.Discard, "", log.LstdFlags)
	}
}

// Charger la configuration depuis le fichier JSON
func loadConfig() (Config, error) {
    var config Config

    // Vérifier si le fichier existe
    if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
        // Fichier non trouvé, créer une configuration par défaut
        config = Config{
            AgentKey:      "",
            EnableLogging: false,
        }

        // Sauvegarder la configuration par défaut dans le fichier
        if err := saveConfig(config); err != nil {
					logger.Printf("failed to create default config: %v", err)
					return config, fmt.Errorf("failed to create default config: %v", err)
        }
        logger.Println("Default config created at", configFilePath)
    } else {
        // Charger la configuration existante
        file, err := ioutil.ReadFile(configFilePath)
        if err != nil {
            return config, err
        }
        if err := json.Unmarshal(file, &config); err != nil {
            return config, err
        }
    }
    return config, nil
}

// Sauvegarder la configuration dans le fichier JSON
func saveConfig(config Config) error {
    file, err := json.MarshalIndent(config, "", "  ")
    if err != nil {
        return err
    }
    return ioutil.WriteFile(configFilePath, file, 0644)
}

// Fonction pour arrêter le monitoring
func stopMonitoring() {
	if monitoring {
		stopChan <- true
		close(stopChan)
	} else {
		logger.Println("Monitoring is not active.")
	}
}

func onReady() {
	iconData, err := ioutil.ReadFile("senhubagent.ico")
	if err != nil {
		logger.Fatalf("Failed to load icon: %v", err)
	} else {
		logger.Println("Icon loaded successfully")
	}
	systray.SetIcon(iconData)
	systray.SetTitle("Senhub Agent")
	systray.SetTooltip("Senhub Agent")

	mSetAgentKey := systray.AddMenuItem("Set Agent Key", "Enter the Agent Key")
	mStart := systray.AddMenuItem("Start Monitoring", "Start Network Monitoring")
	mStop := systray.AddMenuItem("Stop Monitoring", "Stop Network Monitoring")
	mQuit := systray.AddMenuItem("Quit", "Quit the application")

	config, err := loadConfig()
	if err == nil && config.AgentKey != "" {
		AgentKey = config.AgentKey
		logger.Printf("Agent Key loaded from config: %s\n", AgentKey)
		startMonitoring() // Démarrer automatiquement si l'Agent Key est présente
	}

	go func() {
		for {
			select {
			case <-mSetAgentKey.ClickedCh:
				key, err := zenity.Entry("Enter your Agent Key", zenity.Title("Agent Key"))
				if err != nil {
					if err == zenity.ErrCanceled {
						logger.Println("Agent Key input canceled")
					} else {
						logger.Println("Error reading Agent Key:", err)
					}
				} else {
					AgentKey = key
					logger.Printf("Agent Key set: %s\n", AgentKey)
					saveConfig(Config{AgentKey: AgentKey})
					if !monitoring {
						startMonitoring()
					}
				}
			case <-mStart.ClickedCh:
				if AgentKey == "" {
					zenity.Error("Please set an Agent Key before starting the monitoring.")
				} else if !monitoring {
					startMonitoring()
				} else {
					zenity.Info("Monitoring is already running.")
				}
			case <-mStop.ClickedCh:
				if monitoring {
					stopMonitoring()
				} else {
					zenity.Info("Monitoring is not active.")
				}
			case <-mQuit.ClickedCh:
				if monitoring {
					stopMonitoring()
				}
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	logger.Println("Exiting application")
	if monitoring {
		stopMonitoring()
	}
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
	logger.Printf("no active gateway found")
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
		logger.Printf("unsupported OS: %s", runtime.GOOS)
		return 0, 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Printf("ping command failed: %v, output: %s", err, string(output))
			return 0, 0, fmt.Errorf("ping command failed: %v, output: %s", err, string(output))
		}

		outputStr := string(output)
		var averageLatency int
		var packetLoss int

		// Split the output into lines and process each line
		lines := strings.Split(outputStr, "\n")
		for _, line := range lines {
			// Check for packet loss information
			if strings.Contains(line, "Lost =") {
				// Extract packet loss percentage from the line
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "Lost" && i+2 < len(parts) {
						// Example: Lost = 0 (0% loss), so we take "0" from parts[i+2]
						lossString := strings.Trim(parts[i+2], "(%)")
						packetLoss, _ = strconv.Atoi(lossString)
						break
					}
				}
			}

			// Check for average latency information
			if strings.Contains(line, "Average =") {
				// Extract average latency from the line
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "Average" && i+2 < len(parts) {
						// Example: Average = 0ms, so we take "0" from parts[i+2]
						latencyString := strings.TrimSuffix(parts[i+2], "ms")
						averageLatency, _ = strconv.Atoi(latencyString)
						break
					}
				}
			}
		}

		return averageLatency, packetLoss, nil
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
	logger.Printf("no IP address found for URL: %s", pageURL)
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
		logger.Printf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
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
func getURLFromEndpoint(AgentKey string) (string, error) {
	endpoint := fmt.Sprintf("https://nats.sensorfactory.eu:8443/configs?api_key=%s", AgentKey)

	// Debugging: print the generated endpoint
	logger.Printf("Generated Endpoint: %s\n", endpoint)

	// Create a new request
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	// Add the x-api-key header
	req.Header.Set("x-api-key", AgentKey)

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		logger.Printf("error: received status code %d from endpoint", resp.StatusCode)
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
			logger.Println("Error retrieving Wi-Fi signal strength:", err)
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
			logger.Println("Error retrieving Wi-Fi signal strength:", err)
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
			logger.Println("Error retrieving Wi-Fi signal strength:", err)
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
		logger.Printf("unsupported OS: %s\n", runtime.GOOS)
		return 0 // Return 0 if the OS is unsupported
	}

	return 0 // Return 0 if the signal strength cannot be determined
}

// Function to POST metrics to the specified endpoint
func postMetrics(AgentKey string, metrics Metrics) error {
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
	req.Header.Set("x-api-key", AgentKey)

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check if the response status is OK
	if resp.StatusCode != http.StatusOK {
		logger.Printf("error: received status code %d from metrics endpoint", resp.StatusCode)
		return fmt.Errorf("error: received status code %d from metrics endpoint", resp.StatusCode)
	}

	return nil
}

func startMonitoring() {
	if monitoring {
		logger.Println("Monitoring is already running.")
		return
	}

	monitoring = true
	stopChan = make(chan bool)

	go func() {
		for {
			select {
			case <-stopChan:
				logger.Println("Monitoring stopped.")
				monitoring = false
				return
			default:
				// Vérifier que la clé API est définie
				if AgentKey == "" {
					logger.Println("Agent Key not set, cannot start monitoring.")
					return
				}

				// Obtenir l'URL depuis l'endpoint spécifié
				pageURL, err := getURLFromEndpoint(AgentKey)
				if err != nil {
					logger.Println("Error retrieving URL from endpoint:", err)
					return
				}
				logger.Println("Retrieved URL:", pageURL)

				// Récupérer l'adresse IP de la passerelle
				gatewayIP, err := getGatewayIP()
				if err != nil {
					logger.Println("Error retrieving gateway IP address:", err)
					return
				}
				logger.Println("Gateway IP address:", gatewayIP)

				// Ping de la passerelle et récupération des métriques
				gatewayLatency, gatewayPacketLoss, err := ping(gatewayIP)
				if err != nil {
					logger.Println("Error pinging the gateway:", err)
					return
				}
				logger.Printf("Average Latency for Gateway: %d ms\n", gatewayLatency)
				logger.Printf("Packet Loss for Gateway: %d\n", gatewayPacketLoss)

				// Récupérer la force du signal Wi-Fi
				wifiSignalStrength := getWifiSignalStrength()

				// Récupérer le nom d'hôte de l'URL pour le ping
				parsedURL, err := url.Parse(pageURL)
				if err != nil {
					logger.Println("Error parsing URL:", err)
					return
				}

				// Obtenir l'adresse IP de la cible
				targetIP, err := getIPFromURL(parsedURL.Hostname())
				if err != nil {
					logger.Println("Error retrieving IP address from URL:", err)
					return
				}
				logger.Println("Target IP address:", targetIP)

				// Ping de la cible et récupération des métriques
				latency, packetLoss, err := ping(targetIP)
				if err != nil {
					logger.Println("Error pinging the target:", err)
					return
				}
				logger.Printf("Average Latency for %s: %d ms\n", pageURL, latency)
				logger.Printf("Packet Loss for %s: %d\n", pageURL, packetLoss)

				// Mesurer le temps de chargement de la page
				domLoadTime, fullLoadTime, err := measurePageLoad(pageURL)
				if err != nil {
					logger.Println("Error measuring page load times:", err)
					return
				}

				// Préparer les métriques pour l'envoi
				timestamp := time.Now().Format(time.RFC3339)
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

				// Envoyer les métriques
				if err := postMetrics(AgentKey, metrics); err != nil {
					logger.Println("Error posting metrics:", err)
					return
				}

				// Afficher les résultats incluant le timestamp
				logger.Printf("Timestamp of Measurement: %s\n", timestamp)
				logger.Printf("Estimated DOM Load Time for %s: %d ms\n", pageURL, domLoadTime)
				logger.Printf("Full Page Load Time for %s: %d ms\n", pageURL, fullLoadTime)

				// Attendre 5 minutes avant la prochaine itération
				time.Sleep(5 * time.Minute)
			}
		}
	}()
	logger.Println("Monitoring started.")
}


func main() {
    // Charger la configuration et initialiser le logger
    config, err := loadConfig()
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    AgentKey = config.AgentKey
    enableLogging = config.EnableLogging
    initLogger()

    // Vérifier si l'application est lancée en tant que service ou en mode interactif
    isInteractive, err := svc.IsAnInteractiveSession()
    if err != nil {
        logger.Fatalf("failed to determine if running in an interactive session: %v", err)
    }

    if isInteractive {
        // Mode interactif (par exemple pour tester manuellement)
        if runtime.GOOS == "windows" {
            systray.Run(onReady, onExit)
        } else {
            startMonitoring()
        }
    } else {
        // Mode service Windows
        err := runService("SenHubAgentService", &myService{})
        if err != nil {
            logger.Fatalf("Failed to start service: %v", err)
        }
    }
}
