package prtg

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/testUtils"
)

type MockServerResult struct {
	BodyStr  []byte
	BodyJson map[string]interface{}
	Req      *http.Request
}

func TestSyncStrategyPrtg_NewSyncStrategyPrtg(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	agentConfiguration := configuration.NewAgentConfiguration(
		"authKey",
		"serverUrl",
		&logger,
	)

	t.Run("Start", func(t *testing.T) {
		strategy := NewSyncStrategyPrtg(
			agentConfiguration,
			configuration.StorageConfigParams{
				"server_url": "http://localhost:8080",
			},
			&logger,
		)
		if strategy.GetStrategyName() != "prtg" {
			t.Errorf("GetStrategyParams() != prtg: %s", strategy.GetStrategyName())
		}
	})
}

func TestSyncStrategyPrtg_ParseSyncStrategyPrtgParams(t *testing.T) {
	t.Run("Default values", func(t *testing.T) {
		params, err := ParseSyncStrategyPrtgParams(configuration.StorageConfigParams{
			"server_url": "http://localhost:8080",
		})
		if err != nil {
			t.Errorf("ParseSyncStrategyPrtgParams() error = %v", err)
		}
		if params.Interval != DEFAULT_PRTG_INTERVAL {
			t.Errorf("ParseSyncStrategyPrtgParams() Interval = %s", params.Interval)
		}
		if params.RetentionPeriod != DEFAULT_RETENTION_PERIOD {
			t.Errorf("ParseSyncStrategyPrtgParams() RetentionPeriod = %s", params.RetentionPeriod)
		}
	})

	tests := []struct {
		name    string
		config  configuration.StorageConfigParams
		wantErr bool
	}{
		{
			name: "Valid configuration",
			config: configuration.StorageConfigParams{
				"server_url":            "http://localhost:8080",
				"interval":              "10s",
				"data_retention_period": "1h",
			},
			wantErr: false,
		},
		{
			name: "Invalid URL",
			config: configuration.StorageConfigParams{
				"server_url": "localhost:8080",
			},
			wantErr: true,
		},
		{
			name: "Invalid Interval",
			config: configuration.StorageConfigParams{
				"server_url": "http://localhost:8080",
				"interval":   "10",
			},
			wantErr: true,
		},
		{
			name: "Invalid Retention",
			config: configuration.StorageConfigParams{
				"server_url":            "http://localhost:8080",
				"data_retention_period": "1",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSyncStrategyPrtgParams(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSyncStrategyPrtgParams() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSyncStrategyPrtg_AddDataPoints(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	agentConfiguration := configuration.NewAgentConfiguration(
		"authKey",
		"serverUrl",
		&logger,
	)

	strategy := NewSyncStrategyPrtg(
		agentConfiguration,
		configuration.StorageConfigParams{
			"server_url": "http://localhost:8080",
		},
		&logger,
	)

	t.Run("AddDataPoints accepts no value", func(t *testing.T) {
		err := strategy.AddDataPoints(nil)
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}
	})

	t.Run("AddDataPoints accepts empty value", func(t *testing.T) {
		err := strategy.AddDataPoints([]datapoint.DataPoint{})
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}
	})

	t.Run("AddDataPoints accepts valid value", func(t *testing.T) {
		err := strategy.AddDataPoints([]datapoint.DataPoint{
			{
				Name:  "test",
				Value: 1,
			},
		})
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}
	})
}

func TestSyncStrategyPrtg_DoSync(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	agentConfiguration := configuration.NewAgentConfiguration(
		"authKey",
		"serverUrl",
		&logger,
	)

	testServer := testUtils.GetTestHTTPServer("OK", 200)
	defer func() { testServer.Server.Close() }()
	config := configuration.StorageConfigParams{
		"server_url": testServer.URL,
	}

	t.Run("DoSync should call server", func(t *testing.T) {

		strategy := NewSyncStrategyPrtg(
			agentConfiguration,
			config,
			&logger,
		)
		if err := strategy.ValidateConfigParams(config); err != nil {
			t.Errorf("ValidateConfigParams() error = %v", err)
		}
		err := strategy.AddDataPoints([]datapoint.DataPoint{
			{
				Name:      "test",
				Value:     1,
				Timestamp: time.Now(),
			},
		})
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}

		err = strategy.DoSync()
		if err != nil {
			t.Errorf("DoSync() error = %v", err)
		}

		if testServer.LastRequest.Req == nil {
			t.Errorf("DoSync() request is nil")
		}
		if testServer.LastRequest.Req.Method != "POST" {
			t.Errorf("DoSync() request method = %s", testServer.LastRequest.Req.Method)
		}
		if testServer.LastRequest.BodyStr == nil {
			t.Errorf("DoSync() request body is nil")
		}
	})

	t.Run("DoSync sends data", func(t *testing.T) {
		strategy := NewSyncStrategyPrtg(
			agentConfiguration,
			config,
			&logger,
		)
		if err := strategy.ValidateConfigParams(config); err != nil {
			t.Errorf("ValidateConfigParams() error = %v", err)
		}

		err := strategy.AddDataPoints([]datapoint.DataPoint{
			{
				Name:      "test",
				Value:     1,
				Timestamp: time.Now(),
			},
		})
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}

		err = strategy.DoSync()
		if err != nil {
			t.Errorf("DoSync() error = %v", err)
		}

		if testServer.LastRequest.Req == nil {
			t.Errorf("DoSync() request is nil")
		}

		expected := map[string]interface{}{
			"prtg": map[string]interface{}{
				"result": []interface{}{
					map[string]interface{}{
						"channel": "test",
						"value":   float64(1),
						"float":   float64(1),
					},
				},
			},
		}

		if diff := deep.Equal(testServer.LastRequest.BodyJson, expected); diff != nil {
			t.Errorf("DoSync() request body = %v\n%s", diff, testServer.LastRequest.BodyStr)
		}
	})

	t.Run("DoSync sends data with rewritten id", func(t *testing.T) {
		strategy := NewSyncStrategyPrtg(
			agentConfiguration,
			config,
			&logger,
		)
		if err := strategy.ValidateConfigParams(config); err != nil {
			t.Errorf("ValidateConfigParams() error = %v", err)
		}

		err := strategy.AddDataPoints([]datapoint.DataPoint{
			{
				Name:      "test",
				Tags:      []tags.Tag{{Key: "prtg_metric_id", Value: "prtg_test"}},
				Value:     1,
				Timestamp: time.Now(),
			},
		})
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}

		err = strategy.DoSync()
		if err != nil {
			t.Errorf("DoSync() error = %v", err)
		}

		if testServer.LastRequest.Req == nil {
			t.Errorf("DoSync() request is nil")
		}

		expected := map[string]interface{}{
			"prtg": map[string]interface{}{
				"result": []interface{}{
					map[string]interface{}{
						"channel": "prtg_test",
						"value":   float64(1),
						"float":   float64(1),
					},
				},
			},
		}

		if diff := deep.Equal(testServer.LastRequest.BodyJson, expected); diff != nil {
			t.Errorf("DoSync() request body = %v\n%s", diff, testServer.LastRequest.BodyStr)
		}
	})

	t.Run("DoSync sends only latest value", func(t *testing.T) {
		strategy := NewSyncStrategyPrtg(
			agentConfiguration,
			config,
			&logger,
		)
		if err := strategy.ValidateConfigParams(config); err != nil {
			t.Errorf("ValidateConfigParams() error = %v", err)
		}

		err := strategy.AddDataPoints([]datapoint.DataPoint{
			{
				Name:      "test",
				Value:     1,
				Timestamp: time.Now().Add(-2 * time.Second),
			},
			{
				Name:      "test",
				Value:     2,
				Timestamp: time.Now(),
			},
			{
				Name:      "test",
				Value:     3,
				Timestamp: time.Now().Add(-time.Second),
			},
			{
				Name:      "other",
				Value:     4,
				Timestamp: time.Now().Add(-3 * time.Second),
			},
		})
		if err != nil {
			t.Errorf("AddDataPoints() error = %v", err)
		}

		err = strategy.DoSync()
		if err != nil {
			t.Errorf("DoSync() error = %v", err)
		}

		if testServer.LastRequest.Req == nil {
			t.Errorf("DoSync() request is nil")
		}

		expected := map[string]interface{}{
			"prtg": map[string]interface{}{
				"result": []interface{}{
					map[string]interface{}{
						"channel": "test",
						"value":   float64(2),
						"float":   float64(1),
					},
					map[string]interface{}{
						"channel": "other",
						"value":   float64(4),
						"float":   float64(1),
					},
				},
			},
		}

		if diff := deep.Equal(testServer.LastRequest.BodyJson, expected); diff != nil {
			t.Errorf("DoSync() request body = %v\n%s", diff, testServer.LastRequest.BodyStr)
		}
	})
}
