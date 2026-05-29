// senhub-agent/internal/agent/probes/webapp/loadWebAppProbe.go
package webapp

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
	"strings"
	"time"

	"senhub-agent.go/probesdk/configparser"
	"senhub-agent.go/probesdk/datastore"
	"senhub-agent.go/probesdk/logger"
	"senhub-agent.go/probesdk/tags"
	"senhub-agent.go/probesdk/types"
	"senhub-agent.go/probesdk/validators"
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

type LoadWebAppProbeConfig struct {
	URL     string
	Timeout time.Duration
}

type LoadWebAppProbe struct {
	*types.BaseProbe
	rawConfig    map[string]interface{}
	config       LoadWebAppProbeConfig
	moduleLogger *logger.ModuleLogger
}

func NewLoadWebAppProbe(config map[string]interface{}, baseLogger *logger.Logger) (types.Probe, error) {
	parsedConfig, err := parseLoadWebAppProbeConfig(config)
	if err != nil {
		return nil, err
	}

	// Create module-specific logger for loadwebapp probe
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.loadwebapp")

	return &LoadWebAppProbe{
		BaseProbe:    &types.BaseProbe{},
		rawConfig:    config,
		config:       parsedConfig,
		moduleLogger: moduleLogger,
	}, nil
}

func parseLoadWebAppProbeConfig(config map[string]interface{}) (LoadWebAppProbeConfig, error) {
	errs := []error{}
	url, ok := config["url"].(string)
	if !ok || url == "" {
		errs = append(errs, fmt.Errorf("url parameter is required"))
	} else if !validators.IsURL(url) {
		errs = append(errs, fmt.Errorf("url must be a valid URL"))
	}

	timeout := DefaultTimeout
	timeoutVal, ok := config["timeout"]
	if ok && validators.IsDuration(timeoutVal) {
		duration, err := configParser.ParseDuration(timeoutVal)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid timeout value: %w", err))
		} else if duration < MinTimeout || duration > MaxTimeout {
			errs = append(errs, fmt.Errorf("timeout must be between %v and %v seconds", MinTimeout.Seconds(), MaxTimeout.Seconds()))
		} else {
			timeout = duration
		}
	} else if ok {
		errs = append(errs, fmt.Errorf("Invalid timeout value: %v", timeoutVal))
	}

	if len(errs) > 0 {
		return LoadWebAppProbeConfig{}, fmt.Errorf("error parsing config: %v", errs)
	}

	return LoadWebAppProbeConfig{
		URL:     url,
		Timeout: timeout,
	}, nil
}

func (p *LoadWebAppProbe) GetTargetStrategies() []string {
	return []string{"senhub", "prtg", "http", "otlp"}
}

// Note: GetName() is now inherited from BaseProbe and will return the unique
// probe name from configuration (e.g., "load_webapp", "webapp_load2") instead of the
// hardcoded type. This enables proper discriminant tagging for multiple instances.

func (p *LoadWebAppProbe) ShouldStart() bool {
	return true
}

func (p *LoadWebAppProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (p *LoadWebAppProbe) Collect() ([]data_store.DataPoint, error) {
	webappURL := p.config.URL
	metrics, err := p.measurePageLoad(webappURL)
	if err != nil {
		return nil, fmt.Errorf("Error measuring network metrics: %w", err)
	}

	urlTagKey, err := tags.UrlToTagKey(webappURL)
	if err != nil {
		return nil, fmt.Errorf("Error converting URL to tag key: %w", err)
	}
	tags := []tags.Tag{
		{Key: "url", Value: webappURL, Private: false},
		data_store.CreatePrtgMetricIdTag(
			fmt.Sprintf("%s_[name]", urlTagKey)),
	}

	datapoints := []data_store.DataPoint{
		{Name: "dnstime", Timestamp: time.Now(), Value: float32(metrics.dnsDone.Sub(metrics.dnsStart).Milliseconds()), Tags: tags},
		{Name: "connecttime", Timestamp: time.Now(), Value: float32(metrics.connectDone.Sub(metrics.connectStart).Milliseconds()), Tags: tags},
		{Name: "tlstime", Timestamp: time.Now(), Value: float32(metrics.tlsHandshakeDone.Sub(metrics.tlsHandshakeStart).Milliseconds()), Tags: tags},
		{Name: "ttfb", Timestamp: time.Now(), Value: float32(metrics.firstByteDone.Sub(metrics.firstByteStart).Milliseconds()), Tags: tags},
		{Name: "total_time", Timestamp: time.Now(), Value: float32(metrics.completed.Sub(metrics.dnsStart).Milliseconds()), Tags: tags},
	}

	// Enrich datapoints with probe name and type tags
	enrichedDatapoints := p.BaseProbe.EnrichDataPointsWithProbeName(datapoints, p.GetName())

	return enrichedDatapoints, nil
}

func (p *LoadWebAppProbe) measurePageLoad(pageURL string) (*timingMetrics, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		p.moduleLogger.Error().Err(err).Msg("Failed to parse URL")
		return nil, err
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		err := fmt.Errorf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
		p.moduleLogger.Error().Msg(err.Error())
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

	ctx, cancel := context.WithTimeout(context.Background(), p.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		p.moduleLogger.Error().Err(err).Msg("Failed to create request")
		return nil, err
	}

	metrics.firstByteStart = time.Now()
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	// Configuration du client avec gestion des erreurs de certificat
	client := &http.Client{
		Timeout: p.config.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false, // Gardez false pour la sécurité
				MinVersion:         tls.VersionTLS12,
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
				p.moduleLogger.Error().Err(err).Msg("Request timed out")
				return nil, fmt.Errorf("request timed out: %w", err)
			}
		}

		// Gestion des erreurs de certificat
		if strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "certificate") {
			p.moduleLogger.Error().Err(err).Msg("SSL/TLS certificate error")
			return nil, fmt.Errorf("certificate error: %w", err)
		}

		// Gestion des erreurs de contexte
		if ctx.Err() == context.DeadlineExceeded {
			p.moduleLogger.Error().Err(err).Msgf("Request timed out after %v", p.config.Timeout)
			return nil, fmt.Errorf("request timed out after %v: %w", p.config.Timeout, err)
		}

		// Autres erreurs réseau
		p.moduleLogger.Error().Err(err).Msg("Network error occurred")
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	// Vérification du code de statut
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		err := fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		p.moduleLogger.Error().Err(err).Int("status_code", resp.StatusCode).Msg("HTTP error")
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
			p.moduleLogger.Error().Err(err).Msg("Error reading response body")
			return nil, fmt.Errorf("error reading response body: %w", err)
		}
	case <-bodyCtx.Done():
		p.moduleLogger.Error().Msg("Timeout reading response body")
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
