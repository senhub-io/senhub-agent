package kubeletstats

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/probes/types"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// ProbeType is the stable technical identifier used in YAML config and
// license JWT claims.
const ProbeType = "kubelet_stats"

const (
	defaultInterval    = 30 * time.Second
	defaultTokenFile   = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultCAFile      = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	defaultNodeEnv     = "SENHUB_K8S_NODE_NAME"
	nodeIPEnv          = "SENHUB_K8S_NODE_IP"
	defaultKubeletPort = "10250"

	metricScrapeErrors = "senhub.kubelet_stats.scrape_errors"
	metricPodsSeen     = "senhub.kubelet_stats.pods_seen"
)

type probeConfig struct {
	Endpoint           string
	InsecureSkipVerify bool
	IncludeNamespaces  []string
	ExcludeNamespaces  []string
	Interval           time.Duration
}

// KubeletStatsProbe reads the kubelet Summary API of the node it runs on.
type KubeletStatsProbe struct {
	*types.BaseProbe
	cfg          probeConfig
	moduleLogger *logger.ModuleLogger
	endpoint     string
	httpClient   *http.Client
	tokenFile    string
	caFile       string
	// scrapeErrors counts failed scrapes by reason since start: the
	// metric is a counter, so every reason seen keeps being reported.
	scrapeErrors map[string]uint64
}

func NewKubeletStatsProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.kubelet_stats")

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	probe := &KubeletStatsProbe{
		BaseProbe:    &types.BaseProbe{},
		cfg:          cfg,
		moduleLogger: moduleLogger,
		endpoint:     cfg.Endpoint,
		tokenFile:    defaultTokenFile,
		caFile:       defaultCAFile,
	}
	probe.SetProbeType(ProbeType)

	return probe, nil
}

func parseConfig(config map[string]interface{}) (probeConfig, error) {
	cfg := probeConfig{
		Interval: defaultInterval,
	}

	if v, ok := config["endpoint"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.Endpoint = strings.TrimSpace(v)
	}

	if v, ok := config["insecure_skip_verify"].(bool); ok {
		cfg.InsecureSkipVerify = v
	}

	if v, ok := types.IntParam(config, "interval"); ok && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}

	if inc := parseStringSlice(config, "include_namespaces"); inc != nil {
		cfg.IncludeNamespaces = inc
	}
	if exc := parseStringSlice(config, "exclude_namespaces"); exc != nil {
		cfg.ExcludeNamespaces = exc
	}

	// The agent is expected to run as a DaemonSet with this env var set from
	// the pod's spec.nodeName. Tests or hostNetwork users can override it
	// directly with the endpoint parameter.
	// The node IP comes first: a node name is often not resolvable from a
	// pod, while status.hostIP always routes to the kubelet.
	if cfg.Endpoint == "" {
		for _, env := range []string{nodeIPEnv, defaultNodeEnv} {
			if host := strings.TrimSpace(os.Getenv(env)); host != "" {
				cfg.Endpoint = "https://" + net.JoinHostPort(host, defaultKubeletPort)
				break
			}
		}
	}

	return cfg, nil
}

func parseStringSlice(config map[string]interface{}, key string) []string {
	raw := config[key]
	switch v := raw.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (p *KubeletStatsProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

func (p *KubeletStatsProbe) ShouldStart() bool          { return true }
func (p *KubeletStatsProbe) GetInterval() time.Duration { return p.cfg.Interval }

func (p *KubeletStatsProbe) OnStart(_ chan struct{}) error {
	if p.endpoint == "" {
		return fmt.Errorf("kubelet_stats probe: endpoint is empty and %s is not set", defaultNodeEnv)
	}

	if p.httpClient == nil {
		client, err := buildHTTPClient(p.caFile, p.cfg.InsecureSkipVerify)
		if err != nil {
			return fmt.Errorf("kubelet_stats probe: building HTTP client: %w", err)
		}
		p.httpClient = client
	}

	if p.moduleLogger != nil {
		p.moduleLogger.Info().
			Str("endpoint", p.endpoint).
			Bool("insecure_skip_verify", p.cfg.InsecureSkipVerify).
			Msg("Starting kubelet_stats probe")
	}

	return nil
}

func (p *KubeletStatsProbe) OnShutdown(_ context.Context) error {
	return nil
}

func (p *KubeletStatsProbe) Collect() ([]data_store.DataPoint, error) {
	if p.httpClient == nil {
		client, err := buildHTTPClient(p.caFile, p.cfg.InsecureSkipVerify)
		if err != nil {
			return nil, fmt.Errorf("kubelet_stats probe: building HTTP client: %w", err)
		}
		p.httpClient = client
	}

	summary, reason, err := p.scrapeSummary()
	if err != nil {
		if p.moduleLogger != nil {
			p.moduleLogger.Warn().Err(err).
				Str("reason", reason).
				Msg("kubelet_stats: summary scrape failed")
		}

		if p.scrapeErrors == nil {
			p.scrapeErrors = map[string]uint64{}
		}
		p.scrapeErrors[reason]++
		return p.BaseProbe.EnrichDataPointsWithProbeName(p.scrapeErrorPoints(time.Now()), p.GetName()), nil
	}

	now := time.Now()
	points := p.scrapeErrorPoints(now)

	points = append(points, p.collectNode(summary.Node, now)...)

	seenPods := 0
	for i := range summary.Pods {
		pod := &summary.Pods[i]
		if !p.podAllowed(pod) {
			continue
		}

		seenPods++
		points = append(points, p.collectPod(pod, now)...)
		points = append(points, p.collectContainers(pod, now)...)
	}

	points = append(points, data_store.DataPoint{
		Name:      metricPodsSeen,
		Value:     float64(seenPods),
		Timestamp: now,
	})

	return p.BaseProbe.EnrichDataPointsWithProbeName(points, p.GetName()), nil
}

func (p *KubeletStatsProbe) scrapeErrorPoints(now time.Time) []data_store.DataPoint {
	points := make([]data_store.DataPoint, 0, len(p.scrapeErrors))
	for reason, n := range p.scrapeErrors {
		points = append(points, data_store.DataPoint{
			Name:      metricScrapeErrors,
			Value:     float64(n),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "reason", Value: reason}},
		})
	}
	return points
}

func (p *KubeletStatsProbe) scrapeSummary() (*summary, string, error) {
	summaryURL := strings.TrimRight(p.endpoint, "/") + "/stats/summary"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
	if err != nil {
		return nil, "connect", fmt.Errorf("creating kubelet summary request: %w", err)
	}

	// The ServiceAccount token rotates; re-read it for every request instead
	// of caching a value that eventually gets rejected.
	token, err := os.ReadFile(p.tokenFile)
	if err != nil {
		return nil, "connect", fmt.Errorf("reading service account token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, "connect", fmt.Errorf("requesting kubelet summary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "auth", fmt.Errorf("kubelet summary returned status %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "status", fmt.Errorf("kubelet summary returned status %d", resp.StatusCode)
	}

	var s summary
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, "decode", fmt.Errorf("decoding kubelet summary: %w", err)
	}

	return &s, "", nil
}

func buildHTTPClient(caFile string, insecureSkipVerify bool) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	} else {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("parsing CA file %s", caFile)
		}
		tlsConfig.RootCAs = pool
	}

	transport := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsConfig,
	}

	return &http.Client{Transport: transport, Timeout: 10 * time.Second}, nil
}

func (p *KubeletStatsProbe) collectNode(n nodeStats, now time.Time) []data_store.DataPoint {
	base := []tags.Tag{
		{Key: "k8s.node.name", Value: n.NodeName},
	}

	var points []data_store.DataPoint

	if n.CPU != nil && n.CPU.UsageNanoCores != nil {
		points = append(points, data_store.DataPoint{
			Name:      "k8s.node.cpu.usage",
			Value:     float64(*n.CPU.UsageNanoCores) / 1e9,
			Timestamp: now,
			Tags:      base,
		})
	}

	if n.Memory != nil {
		if n.Memory.WorkingSetBytes != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.memory.working_set",
				Value:     float64(*n.Memory.WorkingSetBytes),
				Timestamp: now,
				Tags:      base,
			})
		}
		if n.Memory.AvailableBytes != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.memory.available",
				Value:     float64(*n.Memory.AvailableBytes),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	if n.Fs != nil {
		if n.Fs.UsedBytes != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.filesystem.usage",
				Value:     float64(*n.Fs.UsedBytes),
				Timestamp: now,
				Tags:      base,
			})
		}
		if n.Fs.CapacityBytes != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.filesystem.capacity",
				Value:     float64(*n.Fs.CapacityBytes),
				Timestamp: now,
				Tags:      base,
			})
		}
		if n.Fs.AvailableBytes != nil {
			points = append(points, data_store.DataPoint{
				Name:      "k8s.node.filesystem.available",
				Value:     float64(*n.Fs.AvailableBytes),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	if n.Network != nil {
		rx, rxOK, tx, txOK := networkTotals(n.Network)
		if rxOK {
			points = append(points, networkPoint("k8s.node.network.io", "receive", rx, now, base))
		}
		if txOK {
			points = append(points, networkPoint("k8s.node.network.io", "transmit", tx, now, base))
		}
	}

	return points
}

func (p *KubeletStatsProbe) collectPod(pod *podStats, now time.Time) []data_store.DataPoint {
	base := []tags.Tag{
		{Key: "k8s.namespace.name", Value: pod.PodRef.Namespace},
		{Key: "k8s.pod.name", Value: pod.PodRef.Name},
	}

	var points []data_store.DataPoint

	if pod.CPU != nil && pod.CPU.UsageNanoCores != nil {
		points = append(points, data_store.DataPoint{
			Name:      "k8s.pod.cpu.usage",
			Value:     float64(*pod.CPU.UsageNanoCores) / 1e9,
			Timestamp: now,
			Tags:      base,
		})
	}

	if pod.Memory != nil && pod.Memory.WorkingSetBytes != nil {
		points = append(points, data_store.DataPoint{
			Name:      "k8s.pod.memory.working_set",
			Value:     float64(*pod.Memory.WorkingSetBytes),
			Timestamp: now,
			Tags:      base,
		})
	}

	if pod.Network != nil {
		rx, rxOK, tx, txOK := networkTotals(pod.Network)
		if rxOK {
			points = append(points, networkPoint("k8s.pod.network.io", "receive", rx, now, base))
		}
		if txOK {
			points = append(points, networkPoint("k8s.pod.network.io", "transmit", tx, now, base))
		}
	}

	return points
}

func (p *KubeletStatsProbe) collectContainers(pod *podStats, now time.Time) []data_store.DataPoint {
	var points []data_store.DataPoint

	for i := range pod.Containers {
		c := &pod.Containers[i]

		base := []tags.Tag{
			{Key: "k8s.namespace.name", Value: pod.PodRef.Namespace},
			{Key: "k8s.pod.name", Value: pod.PodRef.Name},
			{Key: "k8s.container.name", Value: c.Name},
		}

		if c.CPU != nil && c.CPU.UsageNanoCores != nil {
			points = append(points, data_store.DataPoint{
				Name:      "container.cpu.usage",
				Value:     float64(*c.CPU.UsageNanoCores) / 1e9,
				Timestamp: now,
				Tags:      base,
			})
		}

		if c.Memory != nil && c.Memory.WorkingSetBytes != nil {
			points = append(points, data_store.DataPoint{
				Name:      "container.memory.working_set",
				Value:     float64(*c.Memory.WorkingSetBytes),
				Timestamp: now,
				Tags:      base,
			})
		}
	}

	return points
}

func (p *KubeletStatsProbe) podAllowed(pod *podStats) bool {
	ns := pod.PodRef.Namespace

	if len(p.cfg.IncludeNamespaces) > 0 && !matchesAny(ns, p.cfg.IncludeNamespaces) {
		return false
	}
	if matchesAny(ns, p.cfg.ExcludeNamespaces) {
		return false
	}

	return true
}

func matchesAny(ns string, patterns []string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, ns); ok {
			return true
		}
	}
	return false
}

// networkTotals reads the default interface the kubelet reports at the top
// level. Summing every interface would count pod traffic twice on a node
// (once on the veth, once on the bridge or uplink); the sum is only the
// fallback when no default interface is reported.
func networkTotals(n *networkStats) (rx uint64, rxOK bool, tx uint64, txOK bool) {
	if n.RxBytes != nil || n.TxBytes != nil {
		if n.RxBytes != nil {
			rx, rxOK = *n.RxBytes, true
		}
		if n.TxBytes != nil {
			tx, txOK = *n.TxBytes, true
		}
		return rx, rxOK, tx, txOK
	}
	if len(n.Interfaces) > 0 {
		for i := range n.Interfaces {
			iface := &n.Interfaces[i]
			if iface.RxBytes != nil {
				rx += *iface.RxBytes
				rxOK = true
			}
			if iface.TxBytes != nil {
				tx += *iface.TxBytes
				txOK = true
			}
		}
		return rx, rxOK, tx, txOK
	}

	if n.RxBytes != nil {
		rx = *n.RxBytes
		rxOK = true
	}
	if n.TxBytes != nil {
		tx = *n.TxBytes
		txOK = true
	}

	return rx, rxOK, tx, txOK
}

func networkPoint(name, direction string, value uint64, now time.Time, base []tags.Tag) data_store.DataPoint {
	t := append(append([]tags.Tag{}, base...), tags.Tag{Key: "direction", Value: direction})
	return data_store.DataPoint{
		Name:      name,
		Value:     float64(value),
		Timestamp: now,
		Tags:      t,
	}
}
