package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// MetricsCollector handles the collection and calculation of all Citrix metrics
type MetricsCollector struct {
	client           CitrixClient
	logger           *logger.ModuleLogger
	helper           *CommonMetricsHelper
	environment      string
	citrixURL        string
	ddcClient        DeliveryControllerClient // Optional: for license metrics via DDC
	siteFilter       string                   // Optional: site name for DDC queries
	licenseServerURL string                   // Optional: direct license server URL (port 8083)
	username         string                   // Credentials for license server NTLM
	password         string
	verifySSL        bool
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(client CitrixClient, baseLogger *logger.Logger) *MetricsCollector {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.metrics")
	return &MetricsCollector{
		client: client,
		logger: moduleLogger,
		helper: NewCommonMetricsHelper(baseLogger),
	}
}

// NewMetricsCollectorWithEnv creates a new metrics collector with environment info
func NewMetricsCollectorWithEnv(client CitrixClient, environment, citrixURL string, baseLogger *logger.Logger) *MetricsCollector {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.metrics")
	return &MetricsCollector{
		client:      client,
		logger:      moduleLogger,
		helper:      NewCommonMetricsHelper(baseLogger),
		environment: environment,
		citrixURL:   citrixURL,
	}
}

// CollectMetrics collects all Citrix metrics using specialized metric modules
func (mc *MetricsCollector) CollectMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	return mc.CollectMetricsWithInventory(ctx, timestamp, nil)
}

// CollectMetricsWithInventory collects all Citrix metrics with inventory service using specialized modules
func (mc *MetricsCollector) CollectMetricsWithInventory(ctx context.Context, timestamp time.Time, inventoryService *InventoryService) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Starting complete Citrix metrics collection using specialized modules")

	var allMetrics []datapoint.DataPoint

	// Collect metrics using specialized modules in metrics_*.go files

	// 1. Infrastructure metrics (instantaneous)
	if infra, err := mc.CollectInfrastructureMetrics(ctx, timestamp, inventoryService); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect infrastructure metrics")
	} else {
		allMetrics = append(allMetrics, infra...)
	}

	// 2. Session metrics (instantaneous + calculated)
	if sessions, err := mc.CollectSessionMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect session metrics")
	} else {
		allMetrics = append(allMetrics, sessions...)
	}

	// 3. Logon metrics (calculated on 2min sliding window)
	if logon, err := mc.CollectLogonMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect logon metrics")
	} else {
		allMetrics = append(allMetrics, logon...)
	}

	// 4. Failure metrics (calculated on 1h sliding window)
	if failures, err := mc.CollectFailureMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect failure metrics")
	} else {
		allMetrics = append(allMetrics, failures...)
	}

	// 5. Load index metrics (instantaneous VDA load data)
	if load, err := mc.CollectLoadMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect load index metrics")
	} else {
		allMetrics = append(allMetrics, load...)
	}

	// 6. License metrics (DDC fallback → License Server direct → skip)
	if mc.ddcClient != nil || mc.licenseServerURL != "" {
		if license, err := mc.CollectLicenseMetrics(ctx, timestamp); err != nil {
			mc.logger.Debug().Err(err).Msg("License metrics collection failed")
		} else if len(license) > 0 {
			allMetrics = append(allMetrics, license...)
		}
	}

	mc.logger.Debug().
		Int("metrics_collected", len(allMetrics)).
		Msg("Complete Citrix metrics collection finished using specialized modules")

	return allMetrics, nil
}

// Legacy functions removed - all metrics collection now handled by specialized modules:
// - metrics_sessions.go: Session metrics and logon duration
// - metrics_infrastructure.go: Infrastructure and machine metrics
// - metrics_failures.go: Connection failure metrics
// - metrics_logon.go: Detailed logon breakdown metrics
// - metrics_common.go: Shared helper functions
