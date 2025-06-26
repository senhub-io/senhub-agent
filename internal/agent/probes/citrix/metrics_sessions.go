package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/tags"
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
	
	// 4. sessions_zombie (ConnectionState == 2 AND disconnected > 24h)
	zombieMetric := mc.calculateSessionsZombie(timestamp, disconnectedSessions)
	metrics = append(metrics, zombieMetric)
	
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


// calculateSessionsZombie counts disconnected sessions older than 24h
func (mc *MetricsCollector) calculateSessionsZombie(timestamp time.Time, sessions []Session) datapoint.DataPoint {
	count := 0
	twentyFourHoursAgo := timestamp.Add(-24 * time.Hour)
	
	for _, session := range sessions {
		if session.ConnectionState == SessionStateDisconnected {
			// Check if session has been disconnected for more than 24h
			// Use ConnectionStateChangeDate from the API response
			if session.SessionStateChangeTime.Before(twentyFourHoursAgo) {
				count++
			}
		}
	}
	
	return datapoint.DataPoint{
		Name:      "sessions_zombie",
		Value:     float32(count),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	}
}

// calculateLogonDurationAvgHourly calculates average logon duration for sessions started in the last hour
func (mc *MetricsCollector) calculateLogonDurationAvgHourly(ctx context.Context, timestamp time.Time) datapoint.DataPoint {
	// Get all sessions (we will filter by StartTime to find sessions started in last hour)
	allSessions, err := mc.client.GetSessions(ctx, time.Time{})
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get sessions for hourly logon duration")
		return datapoint.DataPoint{
			Name:      "logon_duration_avg",
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		}
	}
	
	// Filter sessions opened in the last hour
	oneHourAgo := timestamp.Add(-1 * time.Hour)
	var totalDuration int64
	var validSessionCount int
	
	for _, session := range allSessions {
		// Only include sessions that were started in the last hour AND have valid logon duration
		if session.StartTime.After(oneHourAgo) && session.LogOnDuration > 0 {
			totalDuration += int64(session.LogOnDuration)
			validSessionCount++
			
			mc.logger.Debug().
				Str("session_key", session.SessionKey).
				Time("start_time", session.StartTime).
				Int("logon_duration_ms", session.LogOnDuration).
				Msg("Found session started in last hour with logon data")
		}
	}
	
	var avgDuration float32
	if validSessionCount > 0 {
		avgDuration = float32(totalDuration) / float32(validSessionCount)
		// Round to 2 decimal places
		avgDuration = float32(int(avgDuration*100)) / 100
	} else {
		avgDuration = 0
		mc.logger.Debug().
			Time("since", oneHourAgo).
			Msg("No sessions started in last hour with valid logon duration")
	}
	
	mc.logger.Info().
		Int("total_sessions_checked", len(allSessions)).
		Int("sessions_in_last_hour_with_logon", validSessionCount).
		Float32("avg_logon_duration_ms", avgDuration).
		Time("since", oneHourAgo).
		Msg("✅ Average logon duration calculated for sessions started in last hour")
	
	return datapoint.DataPoint{
		Name:      "logon_duration_avg_1h",
		Value:     avgDuration,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	}
}