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

// calculateLogonDurationAvgHourlyOLD calculates average logon duration using manual calculation from Connection timestamps
// This is the OLD implementation that gives different results than Director Console
// Kept for reference - the issue is that Director uses Session.LogOnDuration pre-calculated field
func (mc *MetricsCollector) calculateLogonDurationAvgHourlyOLD(ctx context.Context, timestamp time.Time) datapoint.DataPoint {
	// Calculate 1-hour sliding window aligned to complete minutes
	// Example: if now is 14:55:43, window is 13:55:00 to 14:55:00
	// Example: if now is 13:00:58, window is 12:00:00 to 13:00:00
	currentMinute := timestamp.Truncate(time.Minute)
	windowEnd := currentMinute
	windowStart := currentMinute.Add(-1 * time.Hour)

	// Log the calculated time window for debugging alignment
	mc.logger.Debug().
		Time("current_time", timestamp).
		Time("window_start_local", windowStart).
		Time("window_end_local", windowEnd).
		Str("window_description", fmt.Sprintf("%s to %s", windowStart.Format("15:04:05"), windowEnd.Format("15:04:05"))).
		Msg("🕐 1-hour average logon duration time window calculated (sliding window on complete minutes)")

	connections, err := mc.client.GetConnections(ctx, windowStart)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connections for hourly logon duration")
		return datapoint.DataPoint{
			Name:      MetricLogonDurationAvg1h,
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
	var totalInWindow, nonHDX, reconnections, incomplete, included int

	for _, conn := range connections {
		if conn.LogOnStartDate.After(windowStart) && conn.LogOnStartDate.Before(windowEnd) {
			totalInWindow++

			// Apply Director Console filtering logic for 1-hour average:
			// - Protocol = "HDX" (only HDX connections)
			// - LogOnEndDate != null (exclude incomplete sessions)
			// - Exclude reconnections (Director excludes them per documentation)
			if conn.Protocol == "HDX" && !conn.IsReconnect && !conn.LogOnEndDate.IsZero() {
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
				} else if conn.IsReconnect {
					reconnections++
					mc.logger.Debug().
						Bool("is_reconnect", conn.IsReconnect).
						Int("connection_id", conn.Id).
						Str("session_key", conn.SessionKey).
						Time("logon_start", conn.LogOnStartDate).
						Msg("❌ Excluding from hourly avg - reconnection excluded per Director documentation")
				} else if conn.LogOnEndDate.IsZero() {
					incomplete++
					mc.logger.Debug().
						Time("logon_start", conn.LogOnStartDate).
						Int("connection_id", conn.Id).
						Str("session_key", conn.SessionKey).
						Msg("⏳ Excluding from hourly avg - logon not yet completed")
				}
			}
		}
	}

	// Log filtering statistics - reconnections excluded per Director documentation
	mc.logger.Debug().
		Int("total_in_window", totalInWindow).
		Int("included", included).
		Int("excluded_non_hdx", nonHDX).
		Int("excluded_reconnections", reconnections).
		Int("excluded_incomplete", incomplete).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("1-hour connection filtering statistics - excludes reconnections per Director documentation")

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
				Str("session_key", conn.SessionKey).
				Time("logon_start", conn.LogOnStartDate).
				Time("logon_end", conn.LogOnEndDate).
				Int("logon_duration_calculated_ms", connectionDuration).
				Float32("logon_duration_calculated_sec", float32(connectionDuration)/1000.0).
				Str("protocol", conn.Protocol).
				Bool("is_reconnect", conn.IsReconnect).
				Msg("📊 Connection included in 1h average calculation")
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

	mc.logger.Debug().
		Int("total_connections_checked", len(connections)).
		Int("connections_in_window", totalInWindow).
		Int("connections_included_after_filter", included).
		Int("connections_with_valid_logon_data", validConnectionCount).
		Int("excluded_non_hdx", nonHDX).
		Int("excluded_reconnections", reconnections).
		Int("excluded_incomplete", incomplete).
		Int64("total_logon_duration_ms", totalDuration).
		Float32("calculated_avg_ms", func() float32 {
			if validConnectionCount > 0 {
				return float32(totalDuration) / float32(validConnectionCount)
			}
			return 0
		}()).
		Float32("final_avg_logon_duration_seconds", avgDurationSeconds).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("🔍 LOGON DURATION DEBUG: Complete calculation breakdown")

	return datapoint.DataPoint{
		Name:      MetricLogonDurationAvg1h,
		Value:     avgDurationSeconds,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	}
}

// calculateLogonDurationAvgHourly calculates average logon duration using Session.LogOnDuration
// Uses Session.StartDate to identify sessions that started (logged on) in the time window
func (mc *MetricsCollector) calculateLogonDurationAvgHourly(ctx context.Context, timestamp time.Time) datapoint.DataPoint {
	// Calculate 1-hour sliding window aligned to complete minutes
	// IMPORTANT: Convert to UTC for comparison with Session.StartTime which is in UTC
	currentMinute := timestamp.Truncate(time.Minute)
	windowEnd := currentMinute.UTC()
	windowStart := currentMinute.Add(-1 * time.Hour).UTC()

	mc.logger.Debug().
		Time("window_start_local", currentMinute.Add(-1*time.Hour)).
		Time("window_end_local", currentMinute).
		Time("window_start_utc", windowStart).
		Time("window_end_utc", windowEnd).
		Msg("🕐 Using Session.StartDate to find sessions that logged on in window (UTC comparison)")

	// Get sessions modified in a wider window to catch all recent activity
	// We'll filter by StartDate to find those that logged on in our 1-hour window
	sessions, err := mc.client.GetSessions(ctx, windowStart.Add(-30*time.Minute))
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get sessions for hourly logon duration")
		return datapoint.DataPoint{
			Name:      MetricLogonDurationAvg1h,
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		}
	}

	mc.logger.Debug().
		Int("total_sessions_retrieved", len(sessions)).
		Msg("Retrieved sessions for logon duration calculation")

	// Debug: Log a few sample sessions to see their StartTime
	for i, session := range sessions {
		if i < 5 { // Log first 5 sessions for debugging
			mc.logger.Debug().
				Str("session_key", session.SessionKey).
				Time("start_time", session.StartTime).
				Int("logon_duration", session.LogOnDuration).
				Str("protocol", session.Protocol).
				Int("connection_state", session.ConnectionState).
				Msg("Sample session data")
		}
	}

	// Filter sessions by StartDate (when they logged on) and other Director criteria
	var validSessions []Session
	var totalInWindow, nonHDX, invalidDuration, included int
	var beforeWindow, afterWindow int

	for _, session := range sessions {
		// Debug window boundaries
		if session.StartTime.Before(windowStart) {
			beforeWindow++
		} else if session.StartTime.After(windowEnd) {
			afterWindow++
		}

		// Check if session started (logged on) in our time window
		if session.StartTime.After(windowStart) && session.StartTime.Before(windowEnd) {
			totalInWindow++

			mc.logger.Debug().
				Str("session_key", session.SessionKey).
				Time("start_date", session.StartTime).
				Int("logon_duration_ms", session.LogOnDuration).
				Float32("logon_duration_sec", float32(session.LogOnDuration)/1000.0).
				Str("protocol", session.Protocol).
				Msg("Session started in time window")

			// Apply Director filtering: HDX protocol only, valid LogOnDuration > 0
			if session.Protocol == "HDX" && session.LogOnDuration > 0 {
				validSessions = append(validSessions, session)
				included++

				mc.logger.Debug().
					Str("session_key", session.SessionKey).
					Time("session_start", session.StartTime).
					Int("logon_duration_ms", session.LogOnDuration).
					Float32("logon_duration_sec", float32(session.LogOnDuration)/1000.0).
					Msg("✅ Session included in logon duration average")
			} else {
				// Count exclusion reasons
				if session.Protocol != "HDX" {
					nonHDX++
					mc.logger.Debug().
						Str("session_key", session.SessionKey).
						Str("protocol", session.Protocol).
						Msg("❌ Excluded: non-HDX protocol")
				}
				if session.LogOnDuration <= 0 {
					invalidDuration++
					mc.logger.Debug().
						Str("session_key", session.SessionKey).
						Int("logon_duration", session.LogOnDuration).
						Msg("❌ Excluded: invalid LogOnDuration")
				}
			}
		}
	}

	mc.logger.Debug().
		Int("total_sessions_in_window", totalInWindow).
		Int("sessions_before_window", beforeWindow).
		Int("sessions_after_window", afterWindow).
		Int("included_sessions", included).
		Int("excluded_non_hdx", nonHDX).
		Int("excluded_invalid_duration", invalidDuration).
		Time("window_start_utc", windowStart).
		Time("window_end_utc", windowEnd).
		Msg("Session filtering complete - using StartDate for time window")

	// Calculate average using Session.LogOnDuration (already in milliseconds)
	var totalDurationMs int64
	var validSessionCount int

	for _, session := range validSessions {
		if session.LogOnDuration > 0 {
			totalDurationMs += int64(session.LogOnDuration)
			validSessionCount++

			mc.logger.Debug().
				Str("session_key", session.SessionKey).
				Int("logon_duration_ms", session.LogOnDuration).
				Float32("logon_duration_sec", float32(session.LogOnDuration)/1000.0).
				Msg("📊 Session.LogOnDuration included in average")
		}
	}

	var avgDurationSeconds float32
	if validSessionCount > 0 {
		// Convert from milliseconds to seconds (Session.LogOnDuration is in ms)
		avgDurationMs := float32(totalDurationMs) / float32(validSessionCount)
		avgDurationSeconds = avgDurationMs / 1000.0
		avgDurationSeconds = roundToTwoDecimals(avgDurationSeconds)
	} else {
		avgDurationSeconds = 0
	}

	mc.logger.Debug().
		Int("total_sessions_checked", len(sessions)).
		Int("sessions_with_valid_logon_duration", validSessionCount).
		Int64("total_duration_ms", totalDurationMs).
		Float32("avg_logon_duration_seconds", avgDurationSeconds).
		Time("window_start_utc", windowStart).
		Time("window_end_utc", windowEnd).
		Msg("✅ Logon duration 1h average calculated using Session.StartDate + Session.LogOnDuration")

	return datapoint.DataPoint{
		Name:      MetricLogonDurationAvg1h,
		Value:     avgDurationSeconds,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	}
}
