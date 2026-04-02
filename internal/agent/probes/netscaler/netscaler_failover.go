// Package netscaler provides monitoring capabilities for Citrix Netscaler (ADC) via NITRO API
package netscaler

import (
	"fmt"
	"strings"

	"github.com/citrix/adc-nitro-go/service"
)

// getClient returns the current NITRO client (thread-safe)
func (p *netscalerProbe) getClient() *service.NitroClient {
	p.clientMu.RLock()
	defer p.clientMu.RUnlock()
	return p.client
}

// switchToNode creates a new NITRO client targeting the given URL,
// authenticates, re-fetches system identity, and refreshes the config cache.
// The old client is logged out on a best-effort basis.
func (p *netscalerProbe) switchToNode(newURL string) error {
	p.logger.Info().
		Str("from", p.activeURL).
		Str("to", newURL).
		Msg("Initiating HA failover to new NetScaler node")

	// Best-effort logout on old client
	p.clientMu.RLock()
	oldClient := p.client
	p.clientMu.RUnlock()
	if oldClient != nil && oldClient.IsLoggedIn() {
		if err := oldClient.Logout(); err != nil {
			p.logger.Debug().Err(err).Msg("Failed to logout from old node (expected if node is down)")
		}
	}

	// Create new NITRO client
	params := service.NitroParams{
		Url:       newURL,
		Username:  p.username,
		Password:  p.password,
		SslVerify: !p.insecureSkipVerify,
		Timeout:   p.timeout,
		LogLevel:  "error",
	}

	newClient, err := service.NewNitroClientFromParams(params)
	if err != nil {
		return fmt.Errorf("failed to create NITRO client for %s: %w", newURL, err)
	}

	// Authenticate with new node
	if err := newClient.Login(); err != nil {
		return fmt.Errorf("failed to authenticate with %s: %w", newURL, err)
	}

	// Swap the client atomically
	p.clientMu.Lock()
	p.client = newClient
	p.activeURL = newURL
	p.clientMu.Unlock()

	// Re-fetch system identity from new node
	if err := p.fetchSystemIdentity(); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to fetch system identity after failover")
	}

	// Refresh config cache from new node
	if err := p.cache.refresh(p.getClient()); err != nil {
		p.logger.Warn().Err(err).Msg("Failed to refresh config cache after failover")
	}

	p.logger.Info().
		Str("active_url", newURL).
		Str("hostname", p.hostname).
		Int("node_id", p.nodeID).
		Msg("HA failover completed successfully")

	return nil
}

// getFailoverURL returns the other URL (the one we're not currently connected to)
func (p *netscalerProbe) getFailoverURL() string {
	if p.secondaryURL == "" {
		return "" // No failover configured
	}
	if p.activeURL == p.baseURL {
		return p.secondaryURL
	}
	return p.baseURL
}

// handleCollectError processes collection errors and triggers failover if needed.
// Returns true if failover was attempted.
func (p *netscalerProbe) handleCollectError() bool {
	failoverURL := p.getFailoverURL()
	if failoverURL == "" {
		return false // No failover configured
	}

	p.consecutiveErrors++
	if p.consecutiveErrors < p.maxFailoverErrors {
		p.logger.Warn().
			Int("consecutive_errors", p.consecutiveErrors).
			Int("max_before_failover", p.maxFailoverErrors).
			Msg("Collection error, will attempt failover after threshold")
		return false
	}

	p.logger.Warn().
		Int("consecutive_errors", p.consecutiveErrors).
		Str("failover_url", failoverURL).
		Msg("Error threshold reached, attempting HA failover")

	if err := p.switchToNode(failoverURL); err != nil {
		p.logger.Error().Err(err).
			Str("failover_url", failoverURL).
			Msg("HA failover failed — both nodes may be down")
		return false
	}

	p.consecutiveErrors = 0
	return true
}

// checkHAPrimaryStatus verifies we're connected to the primary node.
// If connected to secondary, sets pendingFailoverURL so the switch happens
// at the START of the next Collect() cycle (not mid-cycle, which would mix
// data from two different nodes in the same batch).
func (p *netscalerProbe) checkHAPrimaryStatus(haNodeConfigs []map[string]interface{}) {
	if p.secondaryURL == "" {
		return // No failover configured
	}

	for _, nodeConfig := range haNodeConfigs {
		nodeState := getString(nodeConfig, "state")
		nodeIP := getString(nodeConfig, "ipaddress")

		// Find the node we're connected to
		isLocal := false
		if p.hostname != "" {
			nodeName := getString(nodeConfig, "name")
			isLocal = (p.hostname == nodeName)
		}
		if !isLocal && nodeIP != "" {
			isLocal = p.isBaseURLMatchingIP(nodeIP)
		}

		if isLocal && strings.ToUpper(nodeState) == "SECONDARY" {
			failoverURL := p.getFailoverURL()
			if failoverURL != "" {
				p.logger.Warn().
					Str("current_node", p.activeURL).
					Str("current_state", nodeState).
					Str("primary_url", failoverURL).
					Msg("Connected to secondary node — will switch to primary on next collection cycle")
				p.pendingFailoverURL = failoverURL
			}
			return
		}
	}
}
