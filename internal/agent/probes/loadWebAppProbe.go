package probes

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"
	"senhub-agent.go/internal/agent/services/data_store"
)

// Configuration par défaut
const (
	DefaultTimeout = 30 * time.Second
	DefaultInterval = 30 * time.Second
	MinTimeout = 1 * time.Second
	MaxTimeout = 300 * time.Second
	MinInterval = 5 * time.Second
	MaxInterval = 3600 * time.Second
)

type timingMetrics struct {
	dnsStart          time.Time
	dnsDone           time.Time
	connectStart      time.Time
	connectDone       time.Time
	tlsHandshakeStart time.Time
	tlsHandshakeDone  time.Time
	firstByteStart    time.Time
	firstByteDone     time.Time
	completed         time.Time
}

type LoadWebAppProbe struct {
	config map[string]interface{}
}

func NewLoadWebAppProbe(config map[string]interface{}) Probe {
	return &LoadWebAppProbe{
		config: config,
	}
}

func (p *LoadWebAppProbe) GetName() string {
	return "loadWebAppProbe"
}

func (p *LoadWebAppProbe) ShouldStart() bool {
	return true
}

func (p *LoadWebAppProbe) ValidateConfig(config map[string]interface{}) bool {
	if url, ok := config["url"].(string); !ok || url == "" {
		log.Printf("url parameter is required for %s probe", p.GetName())
		return false
	}

	// Validation du timeout si présent
	if timeout, ok := config["timeout"].(float64); ok {
		duration := time.Duration(timeout) * time.Second
		if duration < MinTimeout || duration > MaxTimeout {
			log.Printf("timeout must be between %v and %v seconds for %s probe",
				MinTimeout.Seconds(), MaxTimeout.Seconds(), p.GetName())
			return false
		}
	}

	return true
}

func (p *LoadWebAppProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (p *LoadWebAppProbe) Collect() ([]data_store.DataPoint, error) {
	webappURL := p.config["url"].(string)
	metrics, err := p.measurePageLoad(webappURL)
	if err != nil {
		return nil, fmt.Errorf("Error measuring network metrics: %v", err)
	}

	return []data_store.DataPoint{
		{Name: "webApp_dnstime", Timestamp: time.Now(), Value: float32(metrics.dnsDone.Sub(metrics.dnsStart).Milliseconds())},
		{Name: "webApp_connecttime", Timestamp: time.Now(), Value: float32(metrics.connectDone.Sub(metrics.connectStart).Milliseconds())},
		{Name: "webApp_tlstime", Timestamp: time.Now(), Value: float32(metrics.tlsHandshakeDone.Sub(metrics.tlsHandshakeStart).Milliseconds())},
		{Name: "webApp_ttfb", Timestamp: time.Now(), Value: float32(metrics.firstByteDone.Sub(metrics.firstByteStart).Milliseconds())},
		{Name: "webApp_total_time", Timestamp: time.Now(), Value: float32(metrics.completed.Sub(metrics.dnsStart).Milliseconds())},
	}, nil
}

func (p *LoadWebAppProbe) measurePageLoad(pageURL string) (*timingMetrics, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
	}

	metrics := &timingMetrics{}

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			metrics.dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			metrics.dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			metrics.connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			metrics.connectDone = time.Now()
		},
		TLSHandshakeStart: func() {
			metrics.tlsHandshakeStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			metrics.tlsHandshakeDone = time.Now()
		},
		GotFirstResponseByte: func() {
			metrics.firstByteDone = time.Now()
		},
	}

	// Récupération du timeout depuis la configuration ou utilisation de la valeur par défaut
	timeout := DefaultTimeout
	if timeoutVal, ok := p.config["timeout"].(float64); ok {
		timeout = time.Duration(timeoutVal) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return nil, err
	}

	metrics.firstByteStart = time.Now()

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out after %v", timeout)
		}
		return nil, err
	}
	defer resp.Body.Close()

	// Lecture du corps de la réponse pour s'assurer que la requête est complète
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	metrics.completed = time.Now()

	return metrics, nil
}

func (p *LoadWebAppProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *LoadWebAppProbe) OnShutdown(ctx context.Context) error {
	return nil
}
