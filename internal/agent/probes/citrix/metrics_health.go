package citrix

import (
	"context"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/tags"
)

// CollectHealthMetrics calculates overall health score
func (mc *MetricsCollector) CollectHealthMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Calculating health score")
	
	// Get all necessary data
	machines, err := mc.client.GetMachines(ctx, time.Time{})
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get machines for health score")
		return nil, err
	}
	
	sessions, err := mc.client.GetSessions(ctx, timestamp.Add(-1*time.Hour))
	if err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get sessions for health score")
		return nil, err
	}
	
	// Calculate health score components
	var score float32 = 100.0
	
	// 1. Machine health (40% of score)
	totalMachines := len(machines)
	if totalMachines > 0 {
		healthyMachines := 0
		for _, machine := range machines {
			if machine.RegistrationState == RegistrationStateRegistered && 
			   machine.FaultState == FaultStateNone && 
			   !machine.InMaintenanceMode {
				healthyMachines++
			}
		}
		machineHealthRatio := float32(healthyMachines) / float32(totalMachines)
		machineScore := machineHealthRatio * 40
		score = score - (40 - machineScore)
		
		mc.logger.Debug().
			Int("total_machines", totalMachines).
			Int("healthy_machines", healthyMachines).
			Float32("machine_score", machineScore).
			Msg("Machine health calculated")
	}
	
	// 2. Session health (30% of score)
	activeSessions := 0
	disconnectedSessions := 0
	for _, session := range sessions {
		if session.SessionState == SessionStateConnected || session.SessionState == SessionStateActive {
			activeSessions++
		} else if session.SessionState == SessionStateDisconnected {
			disconnectedSessions++
		}
	}
	
	if activeSessions > 0 {
		// Penalize if more than 10% of sessions are disconnected
		disconnectedRatio := float32(disconnectedSessions) / float32(activeSessions + disconnectedSessions)
		if disconnectedRatio > 0.1 {
			sessionPenalty := (disconnectedRatio - 0.1) * 100
			if sessionPenalty > 30 {
				sessionPenalty = 30
			}
			score = score - sessionPenalty
		}
	}
	
	// 3. UX score (30% of score)
	// Get recent sessions for UX evaluation
	recentSessions, _ := mc.client.GetSessions(ctx, timestamp.Add(-15*time.Minute))
	if len(recentSessions) > 0 {
		poorUXCount := 0
		validSessions := 0
		
		for _, session := range recentSessions {
			if session.LogOnDuration > 0 {
				validSessions++
				if session.LogOnDuration > 60000 { // > 60 seconds
					poorUXCount++
				}
			}
		}
		
		if validSessions > 0 {
			poorUXRatio := float32(poorUXCount) / float32(validSessions)
			uxPenalty := poorUXRatio * 30
			score = score - uxPenalty
			
			mc.logger.Debug().
				Int("poor_ux_sessions", poorUXCount).
				Int("valid_sessions", validSessions).
				Float32("ux_penalty", uxPenalty).
				Msg("UX score calculated")
		}
	}
	
	// Ensure score is between 0 and 100
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}
	
	metric := datapoint.DataPoint{
		Name:      "health_score",
		Value:     roundToTwoDecimals(score),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "analytics"},
		},
	}
	
	mc.logger.Info().
		Float32("health_score", score).
		Msg("Health score calculated")
	
	return []datapoint.DataPoint{metric}, nil
}