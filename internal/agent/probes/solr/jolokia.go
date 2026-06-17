// jolokia.go — minimal Jolokia HTTP client for this probe
package solr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type jolokiaResponse struct {
	Value  json.RawMessage `json:"value"`
	Status int             `json:"status"`
	Error  string          `json:"error"`
}

type jolokiaClient struct {
	baseURL string
	http    *http.Client
}

func (c *jolokiaClient) read(ctx context.Context, mbean, attribute string) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/read/%s/%s", c.baseURL, mbean, attribute)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var jr jolokiaResponse
	if err := json.Unmarshal(body, &jr); err != nil {
		return nil, fmt.Errorf("jolokia parse: %w", err)
	}
	if jr.Status != 200 {
		return nil, fmt.Errorf("jolokia error: %s", jr.Error)
	}
	return jr.Value, nil
}

func (c *jolokiaClient) readInt64(ctx context.Context, mbean, attribute string) (int64, error) {
	raw, err := c.read(ctx, mbean, attribute)
	if err != nil {
		return 0, err
	}
	var v json.Number
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	return v.Int64()
}

func (c *jolokiaClient) readFloat64(ctx context.Context, mbean, attribute string) (float64, error) {
	raw, err := c.read(ctx, mbean, attribute)
	if err != nil {
		return 0, err
	}
	var v json.Number
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	return v.Float64()
}

func (c *jolokiaClient) readMap(ctx context.Context, mbean, attribute string) (map[string]interface{}, error) {
	raw, err := c.read(ctx, mbean, attribute)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	return m, json.Unmarshal(raw, &m)
}
