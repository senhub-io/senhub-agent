package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"
	"math"
	"fmt"
	"syscall"
	"golang.org/x/sys/windows/svc"
	"path/filepath"
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

type MyService struct{}

func (m *MyService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	// Signaler que le service est en cours d'exécution
	s <- svc.Status{State: svc.StartPending}
	go startMonitoring() // Démarrer le monitoring en arrière-plan

	s <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	// Boucle de gestion des requêtes de contrôle du service
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			s <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			s <- svc.Status{State: svc.StopPending}
			stopMonitoring() // Arrêter le monitoring proprement
			return false, 0
		}
	}
	return false, 0
}

var (
	configFilePath string
  logFilePath    string
	AgentKey         string
	enableLogging  bool
	monitoring     bool        // Indicateur pour savoir si le monitoring est actif
	stopChan       chan bool   // Canal pour arrêter le monitoring
	logger         *log.Logger
)

// Initialiser les chemins de fichier
func initPaths() error {
	// Obtenir le chemin de l'exécutable
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execDir := filepath.Dir(execPath)

	// Définir les chemins pour le fichier de configuration et de log
	configFilePath = filepath.Join(execDir, "config.json")
	logFilePath = filepath.Join(execDir, "agent.log")

	return nil
}

// Initialiser le logger
func initLogger() {
    if enableLogging {
        file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
            logger.Fatalf("Failed to open log file: %v", err)
        }
        logger = log.New(file, "", log.LstdFlags)
    } else {
        logger = log.New(ioutil.Discard, "", log.LstdFlags)
    }
}


// Charger la configuration depuis le fichier JSON, ou créer une configuration par défaut si le fichier n'existe pas
func loadConfig() (Config, error) {
	var config Config
	file, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fichier de configuration non trouvé, créer un fichier par défaut
			config = Config{
				AgentKey:      "",
				EnableLogging: false,
			}
			if saveErr := saveConfig(config); saveErr != nil {
				return config, saveErr
			}
			logger.Println("Default config file created.")
			return config, nil
		}
		return config, err
	}
	if err := json.Unmarshal(file, &config); err != nil {
		return config, err
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
    } else {
        logger.Println("Monitoring is not active.")
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


// Fonction pour effectuer un ping sans ouvrir de fenêtre de commande
func ping(ip string) (int, int, error) {
    count := 10 // Nombre de paquets à envoyer
    cmd := exec.Command("ping", "-n", strconv.Itoa(count), ip)

    // Masquer la fenêtre de commande
    cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

    output, err := cmd.CombinedOutput()
    if err != nil {
        log.Printf("ping command failed: %v, output: %s", err, string(output))
        return 0, 0, err
    }

    outputStr := string(output)
    var averageLatency int
    var packetLoss int

    // Analyser la sortie pour extraire les informations de perte de paquets et de latence moyenne
    lines := strings.Split(outputStr, "\n")
    for _, line := range lines {
        if strings.Contains(line, "Lost =") {
            parts := strings.Fields(line)
            for i, part := range parts {
                if part == "Lost" && i+2 < len(parts) {
                    lossString := strings.Trim(parts[i+2], "(%)")
                    packetLoss, _ = strconv.Atoi(lossString)
                    break
                }
            }
        }

        if strings.Contains(line, "Average =") {
            parts := strings.Fields(line)
            for i, part := range parts {
                if part == "Average" && i+2 < len(parts) {
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

// Function to get the local Wi-Fi signal strength
func getWifiSignalStrength() int {
    // Exécuter la commande `netsh wlan show interfaces` pour récupérer les infos Wi-Fi
    cmd := exec.Command("netsh", "wlan", "show", "interfaces")
    output, err := cmd.Output()
    if err != nil {
        logger.Println("Error retrieving Wi-Fi signal strength:", err)
        return 0 // Retourne 0 en cas d'erreur
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
                    logger.Println("Error parsing signal strength:", err)
                    return 0 // Retourne 0 en cas d'erreur d'analyse
                }
                return signalStrength
            }
        }
    }

    // Retourne 0 si la force du signal n'est pas trouvée
    return 0
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
		defer func() {
			monitoring = false
			close(stopChan) // Fermer le canal ici pour éviter des erreurs d’écriture
			logger.Println("Monitoring stopped.")
		}()

		for {
			select {
			case <-stopChan:
				return // Sortie immédiate si stopChan reçoit un signal
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

				// Vérifier `stopChan` avant d'attendre pour permettre un arrêt immédiat
				select {
				case <-stopChan:
					return // Sortie immédiate si arrêt est demandé
				case <-time.After(5 * time.Minute): // Attendre 5 minutes si aucun arrêt n'est demandé
				}
			}
		}
	}()
	logger.Println("Monitoring started.")
}


func main() {
	// Initialiser un logger par défaut pour éviter les erreurs de nil pointer
	logger = log.New(ioutil.Discard, "", log.LstdFlags)

	// Initialiser les chemins de fichier
	if err := initPaths(); err != nil {
		logger.Fatalf("Failed to initialize paths: %v", err)
	}

	// Charger la configuration et initialiser les paramètres
	config, err := loadConfig()
	if err != nil {
		logger.Fatalf("Failed to load or create config: %v", err)
	}

	AgentKey = config.AgentKey
	enableLogging = config.EnableLogging

	// Initialiser le logger en fonction de la configuration
	initLogger()
	logger.Println("Logging is now enabled.")

	// Vérifier si l'application est en mode service
	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		logger.Fatalf("Failed to determine if we are running in an interactive session: %v", err)
	}

	if !isInteractive {
		// Démarrer le service Windows
		runService("SenhubAgentService", &MyService{})
		return
	}

	// Démarrage interactif (pour tester en dehors du mode service)
	go startMonitoring()

	// Canal pour capturer les signaux d'arrêt
	done := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		logger.Println("Received interrupt signal, shutting down...")
		stopMonitoring()
		close(done)
	}()

	<-done
	logger.Println("Program terminated.")
}

func runService(name string, myservice *MyService) {
	err := svc.Run(name, myservice)
	if err != nil {
		logger.Fatalf("Failed to start service: %v", err)
	}
	logger.Println("Service started.")
}
