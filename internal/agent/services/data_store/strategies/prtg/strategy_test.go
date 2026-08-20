package prtg

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
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
			nil,
		)
		if strategy.GetStrategyName() != "prtg" {
			t.Errorf("GetStrategyParams() != prtg: %s", strategy.GetStrategyName())
		}
	})

	// ClientHasBoundedTimeout pins the outbound PRTG push client to a
	// non-zero timeout. Without it a hung PRTG endpoint stalls the sync
	// goroutine forever, silently halting the cache push.
	t.Run("ClientHasBoundedTimeout", func(t *testing.T) {
		strategy := NewSyncStrategyPrtg(
			agentConfiguration,
			configuration.StorageConfigParams{
				"server_url": "http://localhost:8080",
			},
			&logger,
			nil,
		)
		if strategy.http.Timeout <= 0 {
			t.Fatalf("outbound PRTG client has no timeout (got %v); a hung endpoint would block indefinitely", strategy.http.Timeout)
		}
		if strategy.http.Timeout != defaultPushTimeout {
			t.Errorf("expected client timeout %v, got %v", defaultPushTimeout, strategy.http.Timeout)
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
		nil,
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
			nil,
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
			nil,
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
			nil,
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
			nil,
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

// TestChannelName_MatchesTheDefinition pins the convergence of #293: a
// measurement must carry the same channel label whether the agent pushes
// it or PRTG pulls it. The push path used to emit the raw internal id, so
// one device appeared under two channel sets depending on transport.
func TestChannelName_MatchesTheDefinition(t *testing.T) {
	zl := zerolog.New(os.Stderr)
	registry := transformers.NewTransformerRegistry(&zl)
	s := &SyncStrategyPrtg{registry: registry, logger: logger.NewModuleLogger(&zl, "prtg-test")}

	point := datapoint.DataPoint{
		Name: "cpu_core_usage",
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "cpu"},
			{Key: "probe_type", Value: "cpu"},
			{Key: "core", Value: "0"},
		},
	}
	got := s.channelName(point)
	if got == point.Name {
		t.Errorf("channel is still the raw metric id %q; the definition's display name was not resolved", got)
	}

	// An explicit per-probe override still wins: a probe that sets the tag
	// is naming its channel on purpose.
	override := datapoint.DataPoint{
		Name: "cpu_core_usage",
		Tags: []tags.Tag{
			{Key: "probe_type", Value: "cpu"},
			{Key: prtgMetricIDTag, Value: "Custom [name]"},
		},
	}
	if got := s.channelName(override); got != "Custom cpu_core_usage" {
		t.Errorf("override channel=%q, want %q", got, "Custom cpu_core_usage")
	}

	// The invariant is not "some particular string" but "the same string
	// the pull endpoint would show": both resolve through the registry, so
	// a probe type with no definition gets the fallback transformer's
	// rendering on BOTH paths rather than a raw id on one of them.
	unknown := datapoint.DataPoint{
		Name: "whatever",
		Tags: []tags.Tag{{Key: "probe_type", Value: "no-such-probe-type"}},
	}
	transformer, err := registry.LoadTransformer("no-such-probe-type", "friendly")
	if err != nil {
		t.Fatalf("registry refused an unknown probe type: %v", err)
	}
	want := transformer.TransformMetricName("whatever", map[string]string{"probe_type": "no-such-probe-type"})
	if got := s.channelName(unknown); got != want {
		t.Errorf("push channel=%q but the pull path would show %q", got, want)
	}
}
