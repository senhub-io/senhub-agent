package citrix

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// HTTP and OData handling methods for citrixClient

// getODataCollection retrieves items with pagination support
func (c *citrixClient) getODataCollection(ctx context.Context, endpoint, filter string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	params.Add("$top", "1000") // Pagination limit

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}

	// Handle pagination
	for url != "" {
		var response ODataResponse
		nextURL, err := c.performRequest(ctx, url, &response)
		if err != nil {
			return err
		}

		// Append items from this page
		allItems = append(allItems, response.Value...)

		// Check for next page
		url = nextURL
		if url != "" && !strings.HasPrefix(url, "http") {
			// Relative URL, make it absolute
			if strings.HasPrefix(url, "/") {
				url = c.baseURL + url
			} else {
				url = c.baseURL + "/" + url
			}
		}

		c.logger.Debug().
			Int("items_retrieved", len(response.Value)).
			Str("next_url", url).
			Msg("Retrieved page from OData endpoint")
	}

	// Convert to the expected type
	jsonData, err := json.Marshal(allItems)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %v", err)
	}

	if err := json.Unmarshal(jsonData, result); err != nil {
		return fmt.Errorf("failed to unmarshal to target type: %v", err)
	}

	return nil
}

// getODataCollectionUnlimitedWithExpand retrieves ALL items with $expand support without pagination limits
func (c *citrixClient) getODataCollectionUnlimitedWithExpand(ctx context.Context, endpoint, filter, expand string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters - remove $top limit to get all items
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	if expand != "" {
		params.Add("$expand", expand)
	}
	// No $top parameter = get all items

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}
	pageCount := 0

	// Handle pagination
	for url != "" {
		pageCount++
		var response ODataResponse
		nextURL, err := c.performRequest(ctx, url, &response)
		if err != nil {
			return err
		}

		// Append items from this page
		allItems = append(allItems, response.Value...)

		// Check for next page
		url = nextURL
	}

	c.logger.Debug().
		Str("endpoint", endpoint).
		Str("filter", filter).
		Str("expand", expand).
		Int("total_items_retrieved", len(allItems)).
		Int("total_pages", pageCount).
		Msg("Completed unlimited OData collection retrieval with expand")

	// Convert to JSON and back to populate the result struct correctly
	jsonData, err := json.Marshal(allItems)
	if err != nil {
		return fmt.Errorf("failed to marshal OData items: %v", err)
	}

	err = json.Unmarshal(jsonData, result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal OData items: %v", err)
	}

	return nil
}

// getODataCollectionUnlimited retrieves ALL items without pagination limits
func (c *citrixClient) getODataCollectionUnlimited(ctx context.Context, endpoint, filter string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters - remove $top limit to get all items
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	// No $top parameter = get all items

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}
	pageCount := 0

	// Handle pagination
	for url != "" {
		pageCount++
		var response ODataResponse
		nextURL, err := c.performRequest(ctx, url, &response)
		if err != nil {
			return err
		}

		// Append items from this page
		allItems = append(allItems, response.Value...)

		// Check for next page
		url = nextURL
		if url != "" && !strings.HasPrefix(url, "http") {
			// Relative URL, make it absolute
			if strings.HasPrefix(url, "/") {
				url = c.baseURL + url
			} else {
				url = c.baseURL + "/" + url
			}
		}

		c.logger.Debug().
			Int("items_retrieved", len(response.Value)).
			Int("total_items", len(allItems)).
			Int("page_number", pageCount).
			Str("next_url", url).
			Msg("Retrieved unlimited page from OData endpoint")
	}

	c.logger.Debug().
		Int("total_items_retrieved", len(allItems)).
		Int("total_pages", pageCount).
		Str("endpoint", endpoint).
		Msg("Completed unlimited OData collection retrieval")

	// Convert to the expected type
	jsonData, err := json.Marshal(allItems)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %v", err)
	}

	if err := json.Unmarshal(jsonData, result); err != nil {
		return fmt.Errorf("failed to unmarshal to target type: %v", err)
	}

	return nil
}

// getODataCollectionWithExpand retrieves items with $expand parameter for related entities
func (c *citrixClient) getODataCollectionWithExpand(ctx context.Context, endpoint, filter, expand string, result interface{}) error {
	url := c.baseURL + endpoint

	// Add query parameters including $expand
	params := neturl.Values{}
	if filter != "" {
		params.Add("$filter", filter)
	}
	if expand != "" {
		params.Add("$expand", expand)
	}
	// No $top parameter = get all items

	if len(params) > 0 {
		url += "?" + params.Encode()
	}

	var allItems []interface{}
	pageCount := 0

	// Handle pagination
	for url != "" {
		pageCount++
		var response ODataResponse
		nextURL, err := c.performRequest(ctx, url, &response)
		if err != nil {
			return err
		}

		// Append items from this page
		allItems = append(allItems, response.Value...)

		// Check for next page
		url = nextURL
		if url != "" && !strings.HasPrefix(url, "http") {
			// Relative URL, make it absolute
			if strings.HasPrefix(url, "/") {
				url = c.baseURL + url
			} else {
				url = c.baseURL + "/" + url
			}
		}

		c.logger.Debug().
			Int("items_retrieved", len(response.Value)).
			Int("total_items", len(allItems)).
			Int("page_number", pageCount).
			Str("expand", expand).
			Str("next_url", url).
			Msg("Retrieved expanded page from OData endpoint")
	}

	c.logger.Debug().
		Int("total_items_retrieved", len(allItems)).
		Int("total_pages", pageCount).
		Str("endpoint", endpoint).
		Str("expand", expand).
		Msg("Completed expanded OData collection retrieval")

	// Convert to the expected type
	jsonData, err := json.Marshal(allItems)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %v", err)
	}

	if err := json.Unmarshal(jsonData, result); err != nil {
		return fmt.Errorf("failed to unmarshal to target type: %v", err)
	}

	return nil
}

// performRequest executes a single HTTP request with retry logic
func (c *citrixClient) performRequest(ctx context.Context, url string, result interface{}) (nextLink string, err error) {
	var lastErr error

	for attempt := 0; attempt < c.config.MaxRetryAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := time.Duration(float64(time.Second) * c.config.RetryBackoffFactor * float64(attempt))
			c.logger.Debug().
				Int("attempt", attempt+1).
				Dur("delay", delay).
				Msg("Retrying request after delay")

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		nextLink, lastErr = c.doRequest(ctx, url, result)
		if lastErr == nil {
			return nextLink, nil
		}

		c.logger.Debug().
			Err(lastErr).
			Int("attempt", attempt+1).
			Int("max_attempts", c.config.MaxRetryAttempts).
			Msg("OData request failed, will retry if attempts remaining")
	}

	return "", fmt.Errorf("request failed after %d attempts: %v", c.config.MaxRetryAttempts, lastErr)
}

// doRequest performs a single HTTP request
func (c *citrixClient) doRequest(ctx context.Context, url string, result interface{}) (nextLink string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	// Add required Citrix headers
	c.addCitrixHeaders(req)

	// Add authentication headers
	c.addAuthHeaders(req)

	// Debug log the full request details
	c.logger.Debug().
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Str("host", req.Host).
		Interface("headers", req.Header).
		Msg("Sending HTTP request")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error().
			Err(err).
			Str("url", url).
			Str("auth_method", c.config.AuthMethod).
			Msg("HTTP request completely failed")
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	c.logger.Debug().
		Int("status_code", resp.StatusCode).
		Str("url", url).
		Interface("response_headers", resp.Header).
		Msg("Received HTTP response")

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)

		// Log authentication failures with more detail
		if resp.StatusCode == 401 {
			c.logger.Error().
				Int("status_code", resp.StatusCode).
				Str("response_body", string(body)).
				Str("url", url).
				Str("auth_method", c.config.AuthMethod).
				Str("username", c.config.Username).
				Interface("response_headers", resp.Header).
				Interface("request_headers", req.Header).
				Msg("Authentication failed - check credentials and authentication method")
		} else {
			c.logger.Debug().
				Int("status_code", resp.StatusCode).
				Str("response_body", string(body)).
				Str("url", url).
				Msg("HTTP request failed with error response")
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	// Parse the response
	var odataResp ODataResponse
	if err := json.Unmarshal(body, &odataResp); err != nil {
		return "", fmt.Errorf("failed to parse OData response: %v", err)
	}

	// Copy the response to result
	*result.(*ODataResponse) = odataResp

	return odataResp.NextLink, nil
}

// addCitrixHeaders adds required Citrix-specific headers
func (c *citrixClient) addCitrixHeaders(req *http.Request) {
	// Essential headers for OData API
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "SenHub-Citrix-Collector/1.0")
	req.Header.Set("Content-Type", "application/json")

	// Some Citrix environments may require these headers
	if c.config.Environment != "" {
		req.Header.Set("Citrix-InstanceId", c.config.Environment)
	}
}

// addAuthHeaders adds authentication headers based on the configured method
func (c *citrixClient) addAuthHeaders(req *http.Request) {
	switch AuthenticationMethod(c.config.AuthMethod) {
	case AuthMethodBasic:
		req.SetBasicAuth(c.config.Username, c.config.Password)
		c.logger.Debug().
			Str("auth_method", "basic").
			Str("url", req.URL.Path).
			Str("username", c.config.Username).
			Msg("Added basic authentication headers")
	case AuthMethodNTLM:
		// NTLM authentication is handled by the transport layer
		// Set basic auth as fallback - many Citrix servers accept this for NTLM
		req.SetBasicAuth(c.config.Username, c.config.Password)
		c.logger.Debug().
			Str("auth_method", "ntlm_with_basic_fallback").
			Str("url", req.URL.Path).
			Str("username", c.config.Username).
			Str("username_length", fmt.Sprintf("%d", len(c.config.Username))).
			Str("password_length", fmt.Sprintf("%d", len(c.config.Password))).
			Msg("NTLM transport configured with basic auth fallback")
	case AuthMethodKerberos:
		// Kerberos authentication - placeholder for future implementation
		c.logger.Warn().Msg("Kerberos authentication not yet implemented")
	}
}

// formatODataDateTime formats a time for OData filter queries
func formatODataDateTime(t time.Time) string {
	// OData datetime format: 2023-12-20T10:30:00Z
	// Ensure we don't send future dates that might confuse the server
	now := time.Now().UTC()
	if t.After(now) {
		// Note: Future time requested, using current time instead
		t = now
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
