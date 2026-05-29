package citrix

import (
	"context"
	"time"

	"senhub-agent.go/probesdk/datapoint"
	"senhub-agent.go/probesdk/tags"
)

// CollectUXMetrics collects user experience classification metrics
func (mc *MetricsCollector) CollectUXMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting UX classification metrics")

	var metrics []datapoint.DataPoint

	// Get recent sessions for UX metrics (last 15 minutes)
	fifteenMinutesAgo := timestamp.Add(-15 * time.Minute)
	sessions, err := mc.client.GetSessions(ctx, fifteenMinutesAgo)
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get sessions for UX metrics")
		return nil, err
	}

	// Count sessions by UX category based on logon duration
	excellent := 0 // < 15s
	good := 0      // 15-30s
	fair := 0      // 30-60s
	poor := 0      // > 60s

	for _, session := range sessions {
		if session.LogOnDuration <= 0 {
			continue // Skip sessions without logon duration
		}

		durationSeconds := session.LogOnDuration / 1000 // Convert ms to seconds

		switch {
		case durationSeconds < 15:
			excellent++
		case durationSeconds < 30:
			good++
		case durationSeconds < 60:
			fair++
		default:
			poor++
		}
	}

	// Create metrics
	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricUXExcellent,
		Value:     float32(excellent),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "overview"},
		},
	})

	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricUXGood,
		Value:     float32(good),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "overview"},
		},
	})

	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricUXFair,
		Value:     float32(fair),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "overview"},
		},
	})

	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricUXPoor,
		Value:     float32(poor),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "overview"},
		},
	})

	mc.logger.Debug().
		Int("excellent", excellent).
		Int("good", good).
		Int("fair", fair).
		Int("poor", poor).
		Msg("UX metrics calculated")

	return metrics, nil
}
