package citrix

import (
	"context"
	"fmt"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// CollectLogonMetrics collects all logon performance metrics for the last 2 complete minutes.
// This function implements the hybrid approach used by Citrix Director:
// 1. Uses Connections API to identify sessions that started in the time window
// 2. Uses Sessions API to get the pre-calculated LogOnDuration value
// 3. Calculates individual phase durations from Connection timestamps
func (mc *MetricsCollector) CollectLogonMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Collecting logon metrics")

	// STEP 1: Calculate the time window - 2 complete previous minutes aligned on even minutes
	// This matches Citrix Director's calculation methodology:
	// - Director uses a 2-minute sliding window aligned to even minutes
	// - Example: if now is 14:07:30, window is 14:04:00 to 14:06:00 (previous complete even 2-min block)
	// - Example: if now is 14:08:15, window is 14:06:00 to 14:08:00 (current even 2-min block)
	currentMinute := timestamp.Truncate(time.Minute)

	// Align to even minute boundaries (0, 2, 4, 6, 8, 10, etc.)
	minute := currentMinute.Minute()
	if minute%2 != 0 {
		// If we're at an odd minute, go back one minute to align with even boundary
		currentMinute = currentMinute.Add(-1 * time.Minute)
	}

	// CRITICAL: Convert to UTC for API comparison
	// The OData API returns all timestamps in UTC, so we must compare in UTC
	windowEnd := currentMinute.UTC()                         // Even minute boundary in UTC
	windowStart := currentMinute.Add(-2 * time.Minute).UTC() // 2 minutes before in UTC

	// Log the calculated time window for debugging and Director comparison
	mc.logger.Debug().
		Time("current_time", timestamp).
		Time("window_start_local", currentMinute.Add(-2*time.Minute)).
		Time("window_end_local", currentMinute).
		Time("window_start_utc", windowStart).
		Time("window_end_utc", windowEnd).
		Str("window_description", fmt.Sprintf("%s to %s", currentMinute.Add(-2*time.Minute).Format("15:04:05"), currentMinute.Format("15:04:05"))).
		Msg("Logon metrics time window calculated (2 complete previous minutes, aligned to even minutes, UTC comparison)")

	// STEP 2: Retrieve connections from the API
	connections, err := mc.client.GetConnections(ctx, windowStart)
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get connections for logon metrics")
		return nil, err
	}

	// STEP 3: Filter connections using Citrix Director's exact filtering criteria
	// Director applies the following filters for logon duration calculations:
	// 1. Protocol = "HDX" - Only HDX protocol connections are included
	// 2. IsReconnect = false - Reconnections are excluded as they have different logon behavior
	// 3. LogOnEndDate != null - Only completed logons are included (incomplete ones are excluded)
	// 4. LogOnStartDate within time window - Must have started in our 2-minute window
	var recentConnections []Connection
	var totalInWindow, nonHDX, reconnections, incomplete, included int

	for _, conn := range connections {
		// Check if connection started within our time window
		if conn.LogOnStartDate.After(windowStart) && conn.LogOnStartDate.Before(windowEnd) {
			totalInWindow++

			// Apply Director's filtering criteria
			if conn.Protocol == "HDX" && !conn.IsReconnect && !conn.LogOnEndDate.IsZero() {
				// This connection passes all filters and will be included in calculations
				recentConnections = append(recentConnections, conn)
				included++
			} else {
				// Track why connections are excluded for debugging
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
						Msg("Excluding connection - is reconnection (Director excludes these)")
				} else if conn.LogOnEndDate.IsZero() {
					incomplete++
					mc.logger.Debug().
						Time("logon_start", conn.LogOnStartDate).
						Int("connection_id", conn.Id).
						Msg("Excluding connection - logon incomplete (no LogOnEndDate)")
				}
			}
		}
	}

	// Log filtering statistics
	mc.logger.Debug().
		Int("total_in_window", totalInWindow).
		Int("included", included).
		Int("excluded_non_hdx", nonHDX).
		Int("excluded_reconnections", reconnections).
		Int("excluded_incomplete", incomplete).
		Time("window_start_utc", windowStart).
		Time("window_end_utc", windowEnd).
		Msg("Connection filtering statistics")

	if len(recentConnections) == 0 {
		mc.logger.Debug().
			Time("window_start_utc", windowStart).
			Time("window_end_utc", windowEnd).
			Msg("No connections started in 2 complete previous minutes - returning zero metrics")
		// Return zero metrics instead of empty array
		return mc.createZeroLogonMetrics(timestamp), nil
	}

	mc.logger.Debug().
		Int("recent_connections", len(recentConnections)).
		Time("window_start_utc", windowStart).
		Time("window_end_utc", windowEnd).
		Msg("Connections with logon data from 2 complete previous minutes")

	// STEP 4: Calculate all logon breakdown metrics using hybrid approach
	metrics := mc.calculateLogonBreakdownMetricsHybrid(ctx, timestamp, recentConnections, windowStart)

	return metrics, nil
}

// calculateLogonBreakdownMetricsHybrid implements the Citrix Director methodology for logon metrics:
//
// HYBRID APPROACH EXPLANATION:
// - Connections API: Provides accurate temporal data (when logons occurred) and phase breakdowns
// - Sessions API: Provides pre-calculated LogOnDuration field used by Director
//
// CALCULATION METHODOLOGY:
// 1. Use Connections to identify sessions that started logon in the 2-minute window
// 2. Match these Connections with their corresponding Sessions using SessionKey
// 3. Use Session.LogOnDuration for the total duration (matches Director exactly)
// 4. Use Connection timestamps for individual phase breakdowns (not available in Sessions)
//
// This hybrid approach ensures we match Director's total duration while providing phase details.
func (mc *MetricsCollector) calculateLogonBreakdownMetricsHybrid(ctx context.Context, timestamp time.Time, recentConnections []Connection, windowStart time.Time) []datapoint.DataPoint {
	if len(recentConnections) == 0 {
		mc.logger.Debug().Msg("No connections provided for hybrid logon breakdown metrics")
		return nil
	}

	var metrics []datapoint.DataPoint

	// STEP 1: Build a map of session keys from the filtered connections
	// Each connection has a SessionKey that links it to its corresponding Session entity
	sessionKeys := make(map[string]bool)
	connectionsBySessionKey := make(map[string]Connection)

	for _, conn := range recentConnections {
		sessionKeys[conn.SessionKey] = true
		connectionsBySessionKey[conn.SessionKey] = conn
	}

	mc.logger.Debug().
		Int("connections_found", len(recentConnections)).
		Int("unique_sessions", len(sessionKeys)).
		Msg("HYBRID: Collecting unique session keys from connections")

	// STEP 2: Retrieve Sessions from the API
	// We fetch sessions modified in the last 30 minutes to ensure we catch all relevant sessions
	// The Session.ModifiedDate is updated when any aspect of the session changes

	// Fetch sessions with a 30-minute lookback to ensure we don't miss any
	allSessions, err := mc.client.GetSessions(ctx, windowStart.Add(-30*time.Minute))
	if err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get sessions for hybrid logon metrics")
		// If we can't get sessions, return phase metrics without total duration
		return mc.calculatePhaseOnlyMetrics(ctx, timestamp, recentConnections)
	}

	mc.logger.Debug().
		Int("total_sessions_retrieved", len(allSessions)).
		Msg("Retrieved sessions for matching")

	// STEP 3: Match Sessions with Connections using SessionKey
	// IMPORTANT: We match by SessionKey, NOT by StartTime, because:
	// - Session.StartTime represents when the session started (not necessarily when logon occurred)
	// - Connection.LogOnStartDate represents when the logon process began
	// - These timestamps can differ, especially for reconnections or persistent sessions
	var sessionLogonDurations []int
	var matchedSessions int

	for _, session := range allSessions {
		// Match sessions by their SessionKey with our filtered connections
		if sessionKeys[session.SessionKey] {
			// This session corresponds to one of our filtered connections
			if session.LogOnDuration > 0 {
				// Session has a valid pre-calculated LogOnDuration from Director
				sessionLogonDurations = append(sessionLogonDurations, session.LogOnDuration)
				matchedSessions++

				mc.logger.Debug().
					Str("session_key", session.SessionKey).
					Int("logon_duration_ms", session.LogOnDuration).
					Float32("logon_duration_sec", float32(session.LogOnDuration)/1000.0).
					Msg("Session matched with valid LogOnDuration")
			} else {
				// Session matched but has no valid LogOnDuration (shouldn't happen for completed logons)
				mc.logger.Warn().
					Str("session_key", session.SessionKey).
					Msg("Session matched but LogOnDuration is zero or negative")
			}
		}
	}

	mc.logger.Debug().
		Int("connections_count", len(recentConnections)).
		Int("sessions_matched", matchedSessions).
		Int("valid_durations", len(sessionLogonDurations)).
		Msg("Session matching completed")

	// STEP 4: Extract individual phase durations from Connection data
	// The Connection entity provides detailed timestamps for each logon phase.
	// These phase breakdowns are NOT available in the Session entity.
	// We calculate the duration of each phase for averaging across all connections.
	var brokeringDurations []int
	var vmStartDurations []int
	var hdxDurations []int
	var authDurations []int
	var gpoDurations []int
	var scriptDurations []int
	var profileDurations []int
	var interactiveDurations []int

	for _, conn := range recentConnections {
		// BROKERING PHASE: Time to broker the connection to an available resource
		// This is provided as a pre-calculated duration in milliseconds
		if conn.BrokeringDuration > 0 {
			brokeringDurations = append(brokeringDurations, conn.BrokeringDuration)
		}

		// VM START PHASE: Time to start/prepare the virtual machine (if applicable)
		// Note: Often zero for persistent desktops that are already running
		if conn.VMStartStartDate != nil && conn.VMStartEndDate != nil && !conn.VMStartStartDate.IsZero() && !conn.VMStartEndDate.IsZero() {
			vmStartDuration := int(conn.VMStartEndDate.Sub(*conn.VMStartStartDate).Milliseconds())
			if vmStartDuration > 0 {
				vmStartDurations = append(vmStartDurations, vmStartDuration)
			}
		}

		// HDX CONNECTION PHASE: Time to establish the HDX connection
		if !conn.HdxStartDate.IsZero() && !conn.HdxEndDate.IsZero() {
			hdxDuration := int(conn.HdxEndDate.Sub(conn.HdxStartDate).Milliseconds())
			if hdxDuration > 0 {
				hdxDurations = append(hdxDurations, hdxDuration)
			}
		}

		// AUTHENTICATION PHASE: Time for user authentication
		// This is provided as a pre-calculated duration in milliseconds
		if conn.AuthenticationDuration > 0 {
			authDurations = append(authDurations, conn.AuthenticationDuration)
		}

		// GPO (GROUP POLICY) PHASE: Time to apply group policy objects
		if !conn.GpoStartDate.IsZero() && !conn.GpoEndDate.IsZero() {
			gpoDuration := int(conn.GpoEndDate.Sub(conn.GpoStartDate).Milliseconds())
			if gpoDuration > 0 {
				gpoDurations = append(gpoDurations, gpoDuration)
			}
		}

		// LOGON SCRIPTS PHASE: Time to execute logon scripts
		if !conn.LogOnScriptsStartDate.IsZero() && !conn.LogOnScriptsEndDate.IsZero() {
			scriptDuration := int(conn.LogOnScriptsEndDate.Sub(conn.LogOnScriptsStartDate).Milliseconds())
			if scriptDuration > 0 {
				scriptDurations = append(scriptDurations, scriptDuration)
			}
		}

		// PROFILE LOAD PHASE: Time to load the user profile
		if !conn.ProfileLoadStartDate.IsZero() && !conn.ProfileLoadEndDate.IsZero() {
			profileDuration := int(conn.ProfileLoadEndDate.Sub(conn.ProfileLoadStartDate).Milliseconds())
			if profileDuration > 0 {
				profileDurations = append(profileDurations, profileDuration)
			}
		}

		// INTERACTIVE PHASE: Time until the desktop is interactive
		if !conn.InteractiveStartDate.IsZero() && !conn.InteractiveEndDate.IsZero() {
			interactiveDuration := int(conn.InteractiveEndDate.Sub(conn.InteractiveStartDate).Milliseconds())
			if interactiveDuration > 0 {
				interactiveDurations = append(interactiveDurations, interactiveDuration)
			}
		}
	}

	// STEP 5: Create metrics for each phase using Connection data
	// Each phase metric represents the average duration for that phase across all connections
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "brokering", brokeringDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "vmstart", vmStartDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "hdx", hdxDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "authentication", authDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "gpo", gpoDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "scripts", scriptDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "profile", profileDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "interactive", interactiveDurations)...)

	// STEP 6: Use Session.LogOnDuration for total duration (Director's preferred method)
	// Session.LogOnDuration is pre-calculated by Director and is more accurate than summing phases
	var avgSeconds float32
	var durationsUsed int
	var dataSource string

	if len(sessionLogonDurations) > 0 {
		// Use Session.LogOnDuration - this matches Director's calculation exactly
		avgMs := mc.helper.CalculateAverage(sessionLogonDurations)
		avgSeconds = float32(avgMs) / 1000.0
		avgSeconds = roundToTwoDecimals(avgSeconds)
		durationsUsed = len(sessionLogonDurations)
		dataSource = "Session.LogOnDuration"
	} else {
		// ERROR: No sessions found with LogOnDuration - this shouldn't happen for valid connections
		mc.logger.Error().
			Int("connections_processed", len(recentConnections)).
			Int("sessions_retrieved", len(allSessions)).
			Int("sessions_matched", matchedSessions).
			Msg("No sessions with LogOnDuration found - cannot calculate total duration metric")
		// Return only phase metrics without total duration
		return mc.addSessionCountMetric(metrics, timestamp, len(recentConnections))
	}

	// Add the total logon duration metric
	if durationsUsed > 0 {
		metrics = append(metrics, datapoint.DataPoint{
			Name:      MetricLogonDurationTotal,
			Value:     avgSeconds,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
	}

	// Add count of sessions that started logon in the last 2 minutes
	sessionCount := len(recentConnections)
	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricLogonSessionsOpened,
		Value:     float32(sessionCount),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})

	mc.logger.Debug().
		Int("connections_processed", len(recentConnections)).
		Int("sessions_matched", matchedSessions).
		Int("durations_used", durationsUsed).
		Str("data_source", dataSource).
		Float32("avg_total_logon_duration_sec", avgSeconds).
		Int("metrics_generated", len(metrics)).
		Time("window_start_utc", windowStart).
		Msg("Logon breakdown metrics calculated using Connection phases + Session.LogOnDuration total")

	return metrics
}

// calculatePhaseOnlyMetrics creates logon metrics using only Connection data
// This is used as a fallback when Sessions API is unavailable
func (mc *MetricsCollector) calculatePhaseOnlyMetrics(ctx context.Context, timestamp time.Time, recentConnections []Connection) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	// Calculate durations for each phase from Connection data
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
		// Calculate total duration from Connection timestamps
		totalDuration := int(conn.LogOnEndDate.Sub(conn.LogOnStartDate).Milliseconds())
		if totalDuration > 0 {
			totalLogonDurations = append(totalLogonDurations, totalDuration)
		}

		// Calculate individual phase durations
		if conn.BrokeringDuration > 0 {
			brokeringDurations = append(brokeringDurations, conn.BrokeringDuration)
		}

		if conn.VMStartStartDate != nil && conn.VMStartEndDate != nil && !conn.VMStartStartDate.IsZero() && !conn.VMStartEndDate.IsZero() {
			vmStartDuration := int(conn.VMStartEndDate.Sub(*conn.VMStartStartDate).Milliseconds())
			if vmStartDuration > 0 {
				vmStartDurations = append(vmStartDurations, vmStartDuration)
			}
		}

		if !conn.HdxStartDate.IsZero() && !conn.HdxEndDate.IsZero() {
			hdxDuration := int(conn.HdxEndDate.Sub(conn.HdxStartDate).Milliseconds())
			if hdxDuration > 0 {
				hdxDurations = append(hdxDurations, hdxDuration)
			}
		}

		if conn.AuthenticationDuration > 0 {
			authDurations = append(authDurations, conn.AuthenticationDuration)
		}

		if !conn.GpoStartDate.IsZero() && !conn.GpoEndDate.IsZero() {
			gpoDuration := int(conn.GpoEndDate.Sub(conn.GpoStartDate).Milliseconds())
			if gpoDuration > 0 {
				gpoDurations = append(gpoDurations, gpoDuration)
			}
		}

		if !conn.LogOnScriptsStartDate.IsZero() && !conn.LogOnScriptsEndDate.IsZero() {
			scriptDuration := int(conn.LogOnScriptsEndDate.Sub(conn.LogOnScriptsStartDate).Milliseconds())
			if scriptDuration > 0 {
				scriptDurations = append(scriptDurations, scriptDuration)
			}
		}

		if !conn.ProfileLoadStartDate.IsZero() && !conn.ProfileLoadEndDate.IsZero() {
			profileDuration := int(conn.ProfileLoadEndDate.Sub(conn.ProfileLoadStartDate).Milliseconds())
			if profileDuration > 0 {
				profileDurations = append(profileDurations, profileDuration)
			}
		}

		if !conn.InteractiveStartDate.IsZero() && !conn.InteractiveEndDate.IsZero() {
			interactiveDuration := int(conn.InteractiveEndDate.Sub(conn.InteractiveStartDate).Milliseconds())
			if interactiveDuration > 0 {
				interactiveDurations = append(interactiveDurations, interactiveDuration)
			}
		}
	}

	// Create metrics for each phase
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "brokering", brokeringDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "vmstart", vmStartDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "hdx", hdxDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "authentication", authDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "gpo", gpoDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "scripts", scriptDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "profile", profileDurations)...)
	metrics = append(metrics, mc.createLogonPhaseMetrics(timestamp, "interactive", interactiveDurations)...)

	// Add total logon duration using Connection calculation
	if len(totalLogonDurations) > 0 {
		avgMs := mc.helper.CalculateAverage(totalLogonDurations)
		avgSeconds := roundToTwoDecimals(float32(avgMs) / 1000.0)

		metrics = append(metrics, datapoint.DataPoint{
			Name:      MetricLogonDurationTotal,
			Value:     avgSeconds,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
	}

	return mc.addSessionCountMetric(metrics, timestamp, len(recentConnections))
}

// addSessionCountMetric adds the session count metric to the metrics array
func (mc *MetricsCollector) addSessionCountMetric(metrics []datapoint.DataPoint, timestamp time.Time, sessionCount int) []datapoint.DataPoint {
	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricLogonSessionsOpened,
		Value:     float32(sessionCount),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})
	return metrics
}

// createLogonPhaseMetrics creates average metrics for a logon phase
// Each phase duration is converted from milliseconds to seconds and rounded to 2 decimal places
func (mc *MetricsCollector) createLogonPhaseMetrics(timestamp time.Time, phaseName string, durations []int) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	if len(durations) == 0 {
		// No durations available for this phase - return zero metric
		metrics = append(metrics, datapoint.DataPoint{
			Name:      GetLogonPhaseMetricName(phaseName),
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
		return metrics
	}

	// Calculate average duration for this phase
	avgMs := mc.helper.CalculateAverage(durations)
	avgSeconds := roundToTwoDecimals(float32(avgMs) / 1000.0)

	metrics = append(metrics, datapoint.DataPoint{
		Name:      GetLogonPhaseMetricName(phaseName),
		Value:     avgSeconds,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})

	return metrics
}

// createZeroLogonMetrics creates logon metrics with zero values when no sessions are found
// This ensures consistent metric reporting even when no logons occurred
func (mc *MetricsCollector) createZeroLogonMetrics(timestamp time.Time) []datapoint.DataPoint {
	var metrics []datapoint.DataPoint

	phases := []string{"brokering", "vmstart", "hdx", "authentication", "gpo", "scripts", "profile", "interactive"}

	// Create zero metrics for all logon phases
	for _, phase := range phases {
		metrics = append(metrics, datapoint.DataPoint{
			Name:      GetLogonPhaseMetricName(phase),
			Value:     0,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "logon"},
			},
		})
	}

	// Add zero total logon duration metric
	metrics = append(metrics, datapoint.DataPoint{
		Name:      "logon_duration_total",
		Value:     0,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "logon"},
		},
	})

	// Add zero session count metric
	metrics = append(metrics, datapoint.DataPoint{
		Name:      MetricLogonSessionsOpened,
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
