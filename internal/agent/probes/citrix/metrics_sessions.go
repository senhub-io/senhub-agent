package citrix

import (
	"context"
	"fmt"
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
		Name:      "sessions_connected",
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
		Name:      "sessions_disconnected",
		Value:     float32(len(disconnectedSessions)),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	}
	metrics = append(metrics, disconnectedMetric)

	// 4. sessions_zombie (Hidden sessions - need to get ALL sessions to check Hidden flag)
	allSessions, err := mc.client.GetSessions(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get all sessions for zombie calculation")
		// Create zero metric if we can't get sessions
		metrics = append(metrics, datapoint.DataPoint{
			Name:      "sessions_zombie",
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "analytics"},
			},
		})
	} else {
		zombieMetric := mc.calculateSessionsZombie(timestamp, allSessions)
		metrics = append(metrics, zombieMetric)
	}

	// 5. logon_duration_avg (average logon time for sessions in last hour)
	logonDurationMetric := mc.calculateLogonDurationAvgHourly(ctx, timestamp)
	metrics = append(metrics, logonDurationMetric)

	mc.logger.Info().
		Int("sessions_connected", len(connectedSessions)).
		Int("sessions_disconnected", len(disconnectedSessions)).
		Int("metrics_count", len(metrics)).
		Msg("✅ Session metrics collected - simplified and focused")

	return metrics, nil
}

// calculateSessionsZombie counts sessions marked as Hidden (true zombie sessions per Citrix definition)
func (mc *MetricsCollector) calculateSessionsZombie(timestamp time.Time, sessions []Session) datapoint.DataPoint {
	count := 0

	// According to Citrix documentation, zombie sessions are those with Hidden=true
	// These are sessions that are hidden from users and cannot be reconnected
	for _, session := range sessions {
		if session.Hidden {
			count++
		}
	}

	mc.logger.Debug().
		Int("zombie_sessions", count).
		Int("total_sessions_checked", len(sessions)).
		Msg("Zombie session calculation (Hidden=true)")

	return datapoint.DataPoint{
		Name:      "sessions_zombie",
		Value:     float32(count),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "analytics"},
		},
	}
}

// calculateLogonDurationAvgHourly calculates average logon duration using sliding window
// Uses 1-hour sliding window aligned on complete minutes (similar to 2-minute window logic)
func (mc *MetricsCollector) calculateLogonDurationAvgHourly(ctx context.Context, timestamp time.Time) datapoint.DataPoint {
	// Calculate 1-hour sliding window aligned to complete minutes
	// Example: if now is 14:55:43, window is 13:55:00 to 14:55:00
	// Example: if now is 13:00:58, window is 12:00:00 to 13:00:00
	currentMinute := timestamp.Truncate(time.Minute)
	windowEnd := currentMinute
	windowStart := currentMinute.Add(-1 * time.Hour)

	// Log the calculated time window for debugging alignment
	mc.logger.Info().
		Time("current_time", timestamp).
		Time("window_start_local", windowStart).
		Time("window_end_local", windowEnd).
		Str("window_description", fmt.Sprintf("%s to %s", windowStart.Format("15:04:05"), windowEnd.Format("15:04:05"))).
		Msg("🕐 1-hour average logon duration time window calculated (sliding window on complete minutes)")

	connections, err := mc.client.GetConnections(ctx, windowStart)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connections for hourly logon duration")
		return datapoint.DataPoint{
			Name:      "logon_duration_avg_1h",
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		}
	}

	// Filter connections that started within the 1-hour window
	// Apply same filtering logic as 2-minute metrics for consistency
	var recentConnections []Connection
	var totalInWindow, nonHDX, incomplete, included int

	for _, conn := range connections {
		if conn.LogOnStartDate.After(windowStart) && conn.LogOnStartDate.Before(windowEnd) {
			totalInWindow++

			// Apply Director Console filtering logic for 1-hour average:
			// - Protocol = "HDX" (only HDX connections)
			// - LogOnEndDate != null (exclude incomplete sessions)
			// - Include reconnections (Director includes them in average calculation)
			if conn.Protocol == "HDX" && !conn.LogOnEndDate.IsZero() {
				recentConnections = append(recentConnections, conn)
				included++
			} else {
				// Count exclusion reasons for statistics
				if conn.Protocol != "HDX" {
					nonHDX++
					mc.logger.Trace().
						Str("protocol", conn.Protocol).
						Int("connection_id", conn.Id).
						Msg("Excluding from hourly avg - not HDX protocol")
				} else if conn.LogOnEndDate.IsZero() {
					incomplete++
					mc.logger.Trace().
						Time("logon_start", conn.LogOnStartDate).
						Int("connection_id", conn.Id).
						Msg("Excluding from hourly avg - logon not yet completed")
				}
				
				// Note: Reconnections are now included for Director alignment
			}
		}
	}

	// Log filtering statistics - reconnections now included for Director alignment
	mc.logger.Info().
		Int("total_in_window", totalInWindow).
		Int("included", included).
		Int("excluded_non_hdx", nonHDX).
		Int("excluded_incomplete", incomplete).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("1-hour connection filtering statistics - includes reconnections for Director alignment")

	// Calculate average duration from filtered connections
	var totalDuration int64
	var validConnectionCount int

	for _, conn := range recentConnections {
		connectionDuration := int(conn.LogOnEndDate.Sub(conn.LogOnStartDate).Milliseconds())
		if connectionDuration > 0 {
			totalDuration += int64(connectionDuration)
			validConnectionCount++

			mc.logger.Debug().
				Str("connection_id", fmt.Sprintf("%d", conn.Id)).
				Time("logon_start", conn.LogOnStartDate).
				Time("logon_end", conn.LogOnEndDate).
				Int("logon_duration_ms", connectionDuration).
				Msg("Found HDX connection with valid logon duration for 1h average")
		}
	}

	var avgDurationSeconds float32
	if validConnectionCount > 0 {
		// Convert from milliseconds to seconds with 2 decimal places (consistent with 2-minute metrics)
		avgDurationMs := float32(totalDuration) / float32(validConnectionCount)
		avgDurationSeconds = avgDurationMs / 1000.0
		avgDurationSeconds = roundToTwoDecimals(avgDurationSeconds)
	} else {
		avgDurationSeconds = 0
		mc.logger.Debug().
			Time("window_start", windowStart).
			Time("window_end", windowEnd).
			Msg("No connections started in 1-hour window with valid logon duration")
	}

	mc.logger.Info().
		Int("total_connections_checked", len(connections)).
		Int("connections_in_window", totalInWindow).
		Int("connections_with_logon_data", validConnectionCount).
		Float32("avg_logon_duration_seconds", avgDurationSeconds).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("✅ 1-hour average logon duration calculated with consistent windowing")

	return datapoint.DataPoint{
		Name:      "logon_duration_avg_1h",
		Value:     avgDurationSeconds,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	}
}
