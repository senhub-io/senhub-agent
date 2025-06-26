package citrix

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/tags"
)

// CollectLogonMetrics collects all logon performance metrics
func (mc *MetricsCollector) CollectLogonMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting logon metrics")
	
	var metrics []datapoint.DataPoint
	
	// Get recent sessions for logon metrics (last 2 minutes for detailed breakdown)
	twoMinutesAgo := timestamp.Add(-2 * time.Minute)
	sessions, err := mc.client.GetSessions(ctx, twoMinutesAgo)
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get sessions for logon metrics")
		return nil, err
	}
	
	// Filter sessions that started in the last 2 minutes with logon data
	var recentSessions []Session
	for _, session := range sessions {
		if session.StartTime.After(twoMinutesAgo) && session.LogOnDuration > 0 {
			recentSessions = append(recentSessions, session)
		}
	}
	
	if len(recentSessions) == 0 {
		mc.logger.Debug().Msg("No sessions started in last 2 minutes with logon data - returning zero metrics")
		// Return zero metrics instead of empty array
		return mc.createZeroLogonMetrics(timestamp), nil
	}
	
	mc.logger.Debug().Int("recent_sessions", len(recentSessions)).Msg("Sessions with logon data from last 2 minutes")
	
	// Calculate all logon breakdown metrics
	metrics = mc.calculateLogonBreakdownMetrics(timestamp, recentSessions)
	
	return metrics, nil
}


// calculateLogonBreakdownMetrics calculates detailed logon phase metrics from Connection data
func (mc *MetricsCollector) calculateLogonBreakdownMetrics(timestamp time.Time, recentSessions []Session) []datapoint.DataPoint {
	// Get connections for the recent sessions
	twoMinutesAgo := timestamp.Add(-2 * time.Minute)
	connections, err := mc.client.GetConnections(context.Background(), twoMinutesAgo)
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connections for logon breakdown metrics")
		return nil
	}
	
	if len(connections) == 0 {
		mc.logger.Debug().Msg("No connections found for logon breakdown metrics")
		return nil
	}
	
	var metrics []datapoint.DataPoint
	
	// Calculate durations for each phase
	var brokeringDurations []int
	var hdxDurations []int
	var authDurations []int
	var gpoDurations []int
	var scriptDurations []int
	var profileDurations []int
	var interactiveDurations []int
	var totalLogonDurations []int
	
	for _, conn := range connections {
		// Only process connections that started in our time window
		if !conn.LogOnStartDate.After(twoMinutesAgo) {
			continue
		}
		
		// Brokering duration (already provided)
		if conn.BrokeringDuration > 0 {
			brokeringDurations = append(brokeringDurations, conn.BrokeringDuration)
		}
		
		// HDX duration
		if !conn.HdxStartDate.IsZero() && !conn.HdxEndDate.IsZero() {
			hdxDuration := int(conn.HdxEndDate.Sub(conn.HdxStartDate).Milliseconds())
			if hdxDuration > 0 {
				hdxDurations = append(hdxDurations, hdxDuration)
			}
		}
		
		// Authentication duration (already provided)
		if conn.AuthenticationDuration > 0 {
			authDurations = append(authDurations, conn.AuthenticationDuration)
		}
		
		// GPO duration
		if !conn.GpoStartDate.IsZero() && !conn.GpoEndDate.IsZero() {
			gpoDuration := int(conn.GpoEndDate.Sub(conn.GpoStartDate).Milliseconds())
			if gpoDuration > 0 {
				gpoDurations = append(gpoDurations, gpoDuration)
			}
		}
		
		// Scripts duration
		if !conn.LogOnScriptsStartDate.IsZero() && !conn.LogOnScriptsEndDate.IsZero() {
			scriptDuration := int(conn.LogOnScriptsEndDate.Sub(conn.LogOnScriptsStartDate).Milliseconds())
			if scriptDuration > 0 {
				scriptDurations = append(scriptDurations, scriptDuration)
			}
		}
		
		// Profile load duration
		if !conn.ProfileLoadStartDate.IsZero() && !conn.ProfileLoadEndDate.IsZero() {
			profileDuration := int(conn.ProfileLoadEndDate.Sub(conn.ProfileLoadStartDate).Milliseconds())
			if profileDuration > 0 {
				profileDurations = append(profileDurations, profileDuration)
			}
		}
		
		// Interactive duration
		if !conn.InteractiveStartDate.IsZero() && !conn.InteractiveEndDate.IsZero() {
			interactiveDuration := int(conn.InteractiveEndDate.Sub(conn.InteractiveStartDate).Milliseconds())
			if interactiveDuration > 0 {
				interactiveDurations = append(interactiveDurations, interactiveDuration)
			}
		}
		
		// Total logon duration
		if !conn.LogOnStartDate.IsZero() && !conn.LogOnEndDate.IsZero() {
			totalDuration := int(conn.LogOnEndDate.Sub(conn.LogOnStartDate).Milliseconds())
			if totalDuration > 0 {
				totalLogonDurations = append(totalLogonDurations, totalDuration)
			}
		}
	}
	
	// Create metrics for each phase
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "brokering", brokeringDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "hdx", hdxDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "authentication", authDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "gpo", gpoDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "scripts", scriptDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "profile", profileDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "interactive", interactiveDurations)...)
	
	// Add total logon duration metric
	if len(totalLogonDurations) > 0 {
		avg := mc.helper.CalculateAverage(totalLogonDurations)
		avgFloat := float32(avg)
		// Round to 2 decimal places
		avgFloat = float32(int(avgFloat*100)) / 100
		
		metrics = append(metrics, datapoint.DataPoint{
			Name:      "logon_duration_total_2m",
			Value:     avgFloat,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
	}
	
	// Add count of sessions opened in last 2 minutes
	sessionCount := 0
	for _, conn := range connections {
		if conn.LogOnStartDate.After(twoMinutesAgo) {
			sessionCount++
		}
	}
	
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_sessions_opened_2m",
		Value:     float32(sessionCount),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	
	mc.logger.Info().
		Int("connections_processed", len(connections)).
		Int("metrics_generated", len(metrics)).
		Msg("Logon breakdown metrics calculated")
	
	return metrics
}

// createLogonPhaseMetrics creates avg, min, max metrics for a logon phase
func (mc *MetricsCollector) createLogonPhaseMetrics(timestamp time.Time, phaseName string, durations []int) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint
	
	if len(durations) == 0 {
		return metrics
	}
	
	// Calculate statistics
	avg, min, max, _, _ := mc.helper.GetStatistics(durations)
	
	// Convert to float32 and round average to 2 decimal places
	avgFloat := float32(avg)
	avgFloat = float32(int(avgFloat*100)) / 100
	
	// Average
	metrics = append(metrics, datapoint.DataPoint{
		Name:      fmt.Sprintf("logon_%s_avg_2m", phaseName),
		Value:     avgFloat,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	
	// Min
	metrics = append(metrics, datapoint.DataPoint{
		Name:      fmt.Sprintf("logon_%s_min_2m", phaseName),
		Value:     float32(min),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	
	// Max
	metrics = append(metrics, datapoint.DataPoint{
		Name:      fmt.Sprintf("logon_%s_max_2m", phaseName),
		Value:     float32(max),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	
	return metrics
}

// createZeroLogonMetrics creates all logon metrics with zero values when no recent sessions
func (mc *MetricsCollector) createZeroLogonMetrics(timestamp time.Time) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint
	
	phases := []string{"brokering", "hdx", "authentication", "gpo", "scripts", "profile", "interactive"}
	statistics := []string{"avg", "min", "max"}
	
	// Create zero metrics for all logon phases
	for _, phase := range phases {
		for _, stat := range statistics {
			metrics = append(metrics, datapoint.DataPoint{
				Name:      fmt.Sprintf("logon_%s_%s_2m", phase, stat),
				Value:     0,
				Timestamp: timestamp,
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon"},
				},
			})
		}
	}
	
	// Add total logon duration metric
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_duration_total_2m",
		Value:     0,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	
	// Add count of sessions opened in last 2 minutes
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_sessions_opened_2m",
		Value:     0,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	
	mc.logger.Debug().
		Int("zero_metrics_created", len(metrics)).
		Msg("Created zero logon metrics (no recent sessions)")
	
	return metrics
}