package citrix

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// CollectLogonMetrics collects all logon performance metrics
func (mc *MetricsCollector) CollectLogonMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting logon metrics")

	// Calculate the 2 complete previous minutes window aligned on even minutes (like Director)
	// Example: if now is 14:07:30, window is 14:04:00 to 14:06:00
	// Example: if now is 14:08:15, window is 14:06:00 to 14:08:00
	currentMinute := timestamp.Truncate(time.Minute)

	// Align to even minute boundaries
	minute := currentMinute.Minute()
	if minute%2 != 0 {
		// If odd minute, go back one minute to get even boundary
		currentMinute = currentMinute.Add(-1 * time.Minute)
	}

	windowEnd := currentMinute                         // Even minute (14:06:00 or 14:08:00)
	windowStart := currentMinute.Add(-2 * time.Minute) // 2 minutes before (14:04:00 or 14:06:00)

	// Log the calculated time window for debugging alignment with Director
	mc.logger.Info().
		Time("current_time", timestamp).
		Time("window_start_local", windowStart).
		Time("window_end_local", windowEnd).
		Str("window_description", fmt.Sprintf("%s to %s", windowStart.Format("15:04:05"), windowEnd.Format("15:04:05"))).
		Msg("🕐 Logon metrics time window calculated (2 complete previous minutes, aligned to even minutes)")

	connections, err := mc.client.GetConnections(ctx, windowStart)
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get connections for logon metrics")
		return nil, err
	}

	// Filter connections according to Citrix Director logic:
	// - Protocol = "HDX" (only HDX connections)
	// - IsReconnect = false (exclude reconnections per Citrix documentation)
	// - LogOnEndDate != null (exclude incomplete sessions per Citrix documentation)
	// - Started in the 2 complete previous minutes window
	var recentConnections []Connection
	var totalInWindow, nonHDX, reconnections, incomplete, included int

	for _, conn := range connections {
		if conn.LogOnStartDate.After(windowStart) && conn.LogOnStartDate.Before(windowEnd) {
			totalInWindow++

			// Apply Citrix Director filtering logic
			if conn.Protocol == "HDX" && !conn.IsReconnect && !conn.LogOnEndDate.IsZero() {
				recentConnections = append(recentConnections, conn)
				included++
			} else {
				// Count exclusion reasons for statistics
				if conn.Protocol != "HDX" {
					nonHDX++
					mc.logger.Debug().
						Str("protocol", conn.Protocol).
						Int("connection_id", conn.Id).
						Msg("Excluding connection - not HDX protocol")
				} else if conn.IsReconnect {
					reconnections++
					mc.logger.Debug().
						Int("connection_id", conn.Id).
						Msg("Excluding connection - is reconnection (per Citrix Director logic)")
				} else if conn.LogOnEndDate.IsZero() {
					incomplete++
					mc.logger.Debug().
						Time("logon_start", conn.LogOnStartDate).
						Int("connection_id", conn.Id).
						Msg("Excluding connection - logon not yet completed (no LogOnEndDate)")
				}
			}
		}
	}

	// Log filtering statistics
	mc.logger.Info().
		Int("total_in_window", totalInWindow).
		Int("included", included).
		Int("excluded_non_hdx", nonHDX).
		Int("excluded_reconnections", reconnections).
		Int("excluded_incomplete", incomplete).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("Connection filtering statistics")

	if len(recentConnections) == 0 {
		mc.logger.Debug().
			Time("window_start", windowStart).
			Time("window_end", windowEnd).
			Msg("No connections started in 2 complete previous minutes - returning zero metrics")
		// Return zero metrics instead of empty array
		return mc.createZeroLogonMetrics(timestamp), nil
	}

	mc.logger.Debug().
		Int("recent_connections", len(recentConnections)).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Msg("Connections with logon data from 2 complete previous minutes")

	// Calculate all logon breakdown metrics
	metrics := mc.calculateLogonBreakdownMetrics(ctx, timestamp, recentConnections)

	return metrics, nil
}

// calculateLogonBreakdownMetrics calculates detailed logon phase metrics from Connection data
func (mc *MetricsCollector) calculateLogonBreakdownMetrics(ctx context.Context, timestamp time.Time, recentConnections []Connection) []datapoint.DataPoint {
	if len(recentConnections) == 0 {
		mc.logger.Debug().Msg("No connections provided for logon breakdown metrics")
		return nil
	}

	var metrics []datapoint.DataPoint

	// Calculate durations for each phase
	var brokeringDurations []int
	var vmStartDurations []int
	var hdxDurations []int
	var authDurations []int
	var gpoDurations []int
	var scriptDurations []int
	var profileDurations []int
	var interactiveDurations []int
	var totalLogonDurations []int

	for _, conn := range recentConnections {
		// Process only completely finished connections (already filtered by LogOnEndDate)

		// All connections here have LogOnStartDate and LogOnEndDate, so calculate total duration first
		totalDuration := int(conn.LogOnEndDate.Sub(conn.LogOnStartDate).Milliseconds())
		if totalDuration > 0 {
			totalLogonDurations = append(totalLogonDurations, totalDuration)
		}

		// For individual phases, still check if the specific phase has data

		// Brokering duration (already provided)
		if conn.BrokeringDuration > 0 {
			brokeringDurations = append(brokeringDurations, conn.BrokeringDuration)
		}

		// VM Start duration
		if conn.VMStartStartDate != nil && conn.VMStartEndDate != nil && !conn.VMStartStartDate.IsZero() && !conn.VMStartEndDate.IsZero() {
			vmStartDuration := int(conn.VMStartEndDate.Sub(*conn.VMStartStartDate).Milliseconds())
			if vmStartDuration > 0 {
				vmStartDurations = append(vmStartDurations, vmStartDuration)
			}
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
	}

	// Create metrics for each phase (vmstart will be 0 in this environment but still reported)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "brokering", brokeringDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "vmstart", vmStartDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "hdx", hdxDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "authentication", authDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "gpo", gpoDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "scripts", scriptDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "profile", profileDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "interactive", interactiveDurations)...)

	// Add total logon duration metric
	if len(totalLogonDurations) > 0 {
		avgMs := mc.helper.CalculateAverage(totalLogonDurations)
		// Convert from milliseconds to seconds with 2 decimal places
		avgSeconds := float32(avgMs) / 1000.0
		avgSeconds = roundToTwoDecimals(avgSeconds)

		metrics = append(metrics, datapoint.DataPoint{
			Name:      "logon_duration_total",
			Value:     avgSeconds,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
	}

	// Add count of sessions opened in last 2 minutes
	sessionCount := len(recentConnections)

	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_sessions_opened",
		Value:     float32(sessionCount),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})

	mc.logger.Info().
		Int("connections_processed", len(recentConnections)).
		Int("metrics_generated", len(metrics)).
		Msg("Logon breakdown metrics calculated")

	return metrics
}

// createLogonPhaseMetrics creates only avg metrics for a logon phase (simplified naming)
func (mc *MetricsCollector) createLogonPhaseMetrics(timestamp time.Time, phaseName string, durations []int) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	if len(durations) == 0 {
		return metrics
	}

	// Calculate only average (no min/max needed)
	avgMs := mc.helper.CalculateAverage(durations)

	// Convert from milliseconds to seconds with 2 decimal places
	avgSeconds := float32(avgMs) / 1000.0
	avgSeconds = roundToTwoDecimals(avgSeconds)

	// Average only (simplified name without _avg suffix)
	metrics = append(metrics, datapoint.DataPoint{
		Name:      fmt.Sprintf("logon_%s", phaseName),
		Value:     avgSeconds,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})

	return metrics
}

// createZeroLogonMetrics creates only avg logon metrics with zero values when no recent sessions
func (mc *MetricsCollector) createZeroLogonMetrics(timestamp time.Time) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	phases := []string{"brokering", "vmstart", "hdx", "authentication", "gpo", "scripts", "profile", "interactive"}

	// Create zero metrics for all logon phases (avg only, simplified naming)
	for _, phase := range phases {
		metrics = append(metrics, datapoint.DataPoint{
			Name:      fmt.Sprintf("logon_%s", phase),
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
	}

	// Add total logon duration metric
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_duration_total",
		Value:     0,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})

	// Add count of sessions opened in last 2 minutes
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_sessions_opened",
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
