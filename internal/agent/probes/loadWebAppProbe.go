package probes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
)

type LoadWebAppProbe struct {
	config *configuration.RemoteConfiguration
}

func NewLoadWebAppProbe(config *configuration.RemoteConfiguration) Probe {
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

func (p *LoadWebAppProbe) GetInterval() time.Duration {
	return 30 * time.Second
}

func (p *LoadWebAppProbe) Collect() ([]data_store.DataPoint, error) {
	webappURL := "https://tuvalu-legislation.tv"

	domLoadTime, fullLoadTime, err := p.measurePageLoad(webappURL)
	if err != nil {
		return nil, fmt.Errorf("Error measuring page load times: %v", err)
	}

	return []data_store.DataPoint{
		{Name: "webApp_domloadtime", Timestamp: time.Now(), Value: float32(domLoadTime)},
		{Name: "webApp_fullloadtime", Timestamp: time.Now(), Value: float32(fullLoadTime)},
	}, nil
}

func (p *LoadWebAppProbe) measurePageLoad(pageURL string) (float32, float32, error) {
	// Parse the URL to ensure it has the correct format
	parsedURL, err := url.Parse(pageURL)
	if err != nil {
		return 0, 0, err
	}

	// Ensure the URL has the http or https scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		fmt.Errorf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
		return 0, 0, fmt.Errorf("invalid URL scheme: %s, must be http or https", parsedURL.Scheme)
	}

	start := time.Now()
	resp, err := http.Get(pageURL)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	// Simulate DOM load time as half of the total time taken to fetch the page
	fullLoadTime64 := time.Since(start).Seconds() * 1000 // Convert to milliseconds
	domLoadTime64 := fullLoadTime64 / 2                  // Estimate DOM load time

	domLoadTime := float32(domLoadTime64)
	fullLoadTime := float32(fullLoadTime64)

	return domLoadTime, fullLoadTime, nil
}

func (p *LoadWebAppProbe) OnStart(quitChannel chan struct{}) error {
	return nil
}

func (p *LoadWebAppProbe) OnShutdown(ctx context.Context) error {
	return nil
}
