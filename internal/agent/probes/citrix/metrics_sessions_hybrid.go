package citrix

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// calculateLogonDurationAvgHourlyHybrid uses a hybrid approach:
// 1. Gets Connections that started in the 1-hour window
// 2. Gets their associated Sessions to retrieve LogOnDuration
// 3. Calculates average using Session.LogOnDuration
func (mc *MetricsCollector) calculateLogonDurationAvgHourlyHybrid(ctx context.Context, timestamp time.Time) datapoint.DataPoint {
	// Calculate 1-hour sliding window aligned to complete minutes
	currentMinute := timestamp.Truncate(time.Minute)
	windowEnd := currentMinute
	windowStart := currentMinute.Add(-1 * time.Hour)

	mc.logger.Debug().
		Time("window_start_local", windowStart).
		Time("window_end_local", windowEnd).
		Str("window_description", fmt.Sprintf("%s to %s", windowStart.Format("15:04:05"), windowEnd.Format("15:04:05"))).
		Msg("🔄 HYBRID: Getting connections first, then their sessions for LogOnDuration")

	// Step 1: Get connections that started in the window
	connections, err := mc.client.GetConnections(ctx, windowStart)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connections for hybrid logon duration")
		return datapoint.DataPoint{
			Name:      MetricLogonDurationAvg1h,
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		}
	}

	// Step 2: Filter connections and collect unique session keys
	sessionKeys := make(map[string]bool)
	validConnectionCount := 0

	for _, conn := range connections {
		if conn.LogOnStartDate.After(windowStart) && conn.LogOnStartDate.Before(windowEnd) {
			// Apply Director filtering
			if conn.Protocol == "HDX" && !conn.IsReconnect && !conn.LogOnEndDate.IsZero() {
				sessionKeys[conn.SessionKey] = true
				validConnectionCount++

				mc.logger.Debug().
					Str("session_key", conn.SessionKey).
					Int("connection_id", conn.Id).
					Time("logon_start", conn.LogOnStartDate).
					Msg("🔗 Connection included for hybrid calculation")
			}
		}
	}

	mc.logger.Debug().
		Int("valid_connections", validConnectionCount).
		Int("unique_sessions", len(sessionKeys)).
		Msg("Filtered connections for hybrid approach")

	if len(sessionKeys) == 0 {
		return datapoint.DataPoint{
			Name:      MetricLogonDurationAvg1h,
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		}
	}

	// Step 3: Get all sessions (we'll filter by session key)
	sessions, err := mc.client.GetSessions(ctx, windowStart.Add(-30*time.Minute))
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get sessions for hybrid logon duration")
		return datapoint.DataPoint{
			Name:      MetricLogonDurationAvg1h,
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		}
	}

	// Step 4: Match sessions with our connection session keys and calculate average
	var totalDurationMs int64
	var matchedSessions int

	for _, session := range sessions {
		if sessionKeys[session.SessionKey] && session.LogOnDuration > 0 {
			totalDurationMs += int64(session.LogOnDuration)
			matchedSessions++

			mc.logger.Debug().
				Str("session_key", session.SessionKey).
				Int("logon_duration_ms", session.LogOnDuration).
				Float32("logon_duration_sec", float32(session.LogOnDuration)/1000.0).
				Msg("✅ HYBRID: Matched session with LogOnDuration")
		}
	}

	var avgDurationSeconds float32
	if matchedSessions > 0 {
		avgDurationMs := float32(totalDurationMs) / float32(matchedSessions)
		avgDurationSeconds = avgDurationMs / 1000.0
		avgDurationSeconds = roundToTwoDecimals(avgDurationSeconds)
	}

	mc.logger.Debug().
		Int("connections_found", validConnectionCount).
		Int("sessions_matched", matchedSessions).
		Int64("total_duration_ms", totalDurationMs).
		Float32("avg_logon_duration_seconds", avgDurationSeconds).
		Msg("✅ HYBRID: Logon duration calculated using Connections + Session.LogOnDuration")

	return datapoint.DataPoint{
		Name:      MetricLogonDurationAvg1h,
		Value:     avgDurationSeconds,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	}
}
