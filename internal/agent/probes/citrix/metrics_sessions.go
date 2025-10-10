package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// CollectSessionMetrics collects all session-related metrics using optimized OData queries
func (mc *MetricsCollector) CollectSessionMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting session metrics")

	var metrics []datapoint.DataPoint

	// 1. sessions_connected (matches portal "Sessions Connected" = ConnectionState 5)
	connectedSessions, err := mc.client.GetSessionsByConnectionState(ctx, []int{SessionStateActive})
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get connected sessions")
		return nil, err
	}

	connectedMetric := datapoint.DataPoint{
		Name:      MetricSessionsConnected,
		Value:     float32(len(connectedSessions)),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	}
	metrics = append(metrics, connectedMetric)

	// Note: Removed simultaneous session metrics - unclear business value

	// 3. sessions_disconnected (ConnectionState == 2) - optimized query
	disconnectedSessions, err := mc.client.GetSessionsByConnectionState(ctx, []int{SessionStateDisconnected})
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get disconnected sessions")
		return nil, err
	}
	disconnectedMetric := datapoint.DataPoint{
		Name:      MetricSessionsDisconnected,
		Value:     float32(len(disconnectedSessions)),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	}
	metrics = append(metrics, disconnectedMetric)

	// Sessions zombie metric removed - rarely used and not operationally relevant

	// 5. logon_duration_avg (average logon time for sessions in last hour)
	// Using hybrid approach: Connections + Session.LogOnDuration
	logonDurationMetric := mc.calculateLogonDurationAvgHourlyHybrid(ctx, timestamp)
	metrics = append(metrics, logonDurationMetric)

	mc.logger.Debug().
		Int("sessions_connected", len(connectedSessions)).
		Int("sessions_disconnected", len(disconnectedSessions)).
		Int("metrics_count", len(metrics)).
		Msg("✅ Session metrics collected - simplified and focused")

	return metrics, nil
}

// calculateSessionsZombie function removed - zombie sessions metric not operationally relevant

