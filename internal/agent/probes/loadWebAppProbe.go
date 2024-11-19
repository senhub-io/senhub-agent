package probes

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/services/logger"
	"strings"
	"time"
)

// Default configuration
const (
	DefaultTimeout  = 30 * time.Second
	DefaultInterval = 30 * time.Second
	MinTimeout      = 1 * time.Second
	MaxTimeout      = 300 * time.Second
	MinInterval     = 5 * time.Second
	MaxInterval     = 3600 * time.Second
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
	logger *logger.Logger
}

func NewLoadWebAppProbe(config map[string]interface{}, logger *logger.Logger) Probe {
	return &LoadWebAppProbe{
		config: config,
		logger: logger,
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
		p.logger.Error().Msgf("url parameter is required for %s probe", p.GetName())
		return false
	}

	// Validate timeout if present
	if timeout, ok := config["timeout"].(float64); ok {
		duration := time.Duration(timeout) * time.Second
		if duration < MinTimeout || duration > MaxTimeout {
			p.logger.Info().Msgf("timeout must be between %v and %v seconds for %s probe",
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
		p.logger.Error().Err(err).Msg("Error measuring network metrics: %v")
		return nil, err
	}

	tags := map[string]string{
		"url": webappURL,
	}

	return []data_store.DataPoint{
		{Name: "dnstime", Timestamp: time.Now(), Value: float32(metrics.dnsDone.Sub(metrics.dnsStart).Milliseconds()), Tags: tags},
		{Name: "connecttime", Timestamp: time.Now(), Value: float32(metrics.connectDone.Sub(metrics.connectStart).Milliseconds()), Tags: tags},
		{Name: "tlstime", Timestamp: time.Now(), Value: float32(metrics.tlsHandshakeDone.Sub(metrics.tlsHandshakeStart).Milliseconds()), Tags: tags},
		{Name: "ttfb", Timestamp: time.Now(), Value: float32(metrics.firstByteDone.Sub(metrics.firstByteStart).Milliseconds()), Tags: tags},
		{Name: "total_time", Timestamp: time.Now(), Value: float32(metrics.completed.Sub(metrics.dnsStart).Milliseconds()), Tags: tags},
	}, nil
}

func (p *LoadWebAppProbe) measurePageLoad(pageURL string) (*timingMetrics, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to parse URL")
		return nil, err
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		err := fmt.Errorf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
		p.logger.Error().Msg(err.Error())
		return nil, err
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

	timeout := DefaultTimeout
	if timeoutVal, ok := p.config["timeout"].(float64); ok {
		timeout = time.Duration(timeoutVal) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		p.logger.Error().Err(err).Msg("Failed to create request")
		return nil, err
	}

	metrics.firstByteStart = time.Now()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	// Configuration du client avec gestion des erreurs de certificat
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false, // Gardez false pour la sécurité
			},
			// Ajout de timeouts plus spécifiques
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     true, // Pour éviter la réutilisation des connexions
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		// Gestion spécifique des différents types d'erreurs
		var netErr net.Error
		if errors.As(err, &netErr) {
			if netErr.Timeout() {
				p.logger.Error().Err(err).Msg("Request timed out")
				return nil, fmt.Errorf("request timed out: %w", err)
			}
		}

		// Gestion des erreurs de certificat
		if strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "certificate") {
			p.logger.Error().Err(err).Msg("SSL/TLS certificate error")
			return nil, fmt.Errorf("certificate error: %w", err)
		}

		// Gestion des erreurs de contexte
		if ctx.Err() == context.DeadlineExceeded {
			p.logger.Error().Err(err).Msgf("Request timed out after %v", timeout)
			return nil, fmt.Errorf("request timed out after %v: %w", timeout, err)
		}

		// Autres erreurs réseau
		p.logger.Error().Err(err).Msg("Network error occurred")
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	// Vérification du code de statut
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		err := fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		p.logger.Error().Err(err).Int("status_code", resp.StatusCode).Msg("HTTP error")
		return nil, err
	}

	// Lecture du corps avec timeout
	bodyCtx, bodyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bodyCancel()

	bodyDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, resp.Body)
		bodyDone <- err
	}()

	select {
	case err := <-bodyDone:
		if err != nil {
			p.logger.Error().Err(err).Msg("Error reading response body")
			return nil, fmt.Errorf("error reading response body: %w", err)
		}
	case <-bodyCtx.Done():
		p.logger.Error().Msg("Timeout reading response body")
		return nil, fmt.Errorf("timeout reading response body")
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
