package citrix

import (
	"context"
	"fmt"
	"sort"
	"time"

	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// State name mappings for better metrics labeling
var (

	registrationStateNames = map[int]string{
		RegistrationStateUnregistered: "unregistered",
		RegistrationStateRegistered:   "registered",
		RegistrationStateAgentError:   "agent_error",
	}

	faultStateNames = map[int]string{
		FaultStateUnknown:       "unknown",
		FaultStateNone:          "healthy",
		FaultStateFailedToStart: "failed_to_start",
		FaultStateStuckOnBoot:   "stuck_on_boot",
		FaultStateUnregistered:  "unregistered",
		FaultStateMaxCapacity:   "max_capacity",
		FaultStateVMNotFound:    "vm_not_found",
	}

	failureCategoryNames = map[int]string{
		FailureCategoryClientConnection: "client_connection_failures",
		FailureCategoryConfiguration:    "configuration_errors",
		FailureCategoryMachine:         "machine_failures",
		FailureCategoryCapacity:        "unavailable_capacity",
		FailureCategoryLicense:         "unavailable_licenses",
		FailureCategoryOther:           "other_failures",
	}
)

// MetricsCollector handles the collection and calculation of all Citrix metrics
type MetricsCollector struct {
	client      CitrixClient
	logger      *logger.ModuleLogger
	helper      *CommonMetricsHelper
	environment string
	citrixURL   string
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(client CitrixClient, baseLogger *logger.Logger) *MetricsCollector {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.metrics")
	return &MetricsCollector{
		client: client,
		logger: moduleLogger,
		helper: NewCommonMetricsHelper(baseLogger),
	}
}

// NewMetricsCollectorWithEnv creates a new metrics collector with environment info
func NewMetricsCollectorWithEnv(client CitrixClient, environment, citrixURL string, baseLogger *logger.Logger) *MetricsCollector {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.metrics")
	return &MetricsCollector{
		client:      client,
		logger:      moduleLogger,
		helper:      NewCommonMetricsHelper(baseLogger),
		environment: environment,
		citrixURL:   citrixURL,
	}
}

// CollectMetrics collects all Citrix metrics (simplified - no frequency logic)
func (mc *MetricsCollector) CollectMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Starting complete Citrix metrics collection")
	
	var allMetrics []datapoint.DataPoint
	
	// Collect ALL metrics every time - let the probe interval control frequency
	
	// 1. Infrastructure metrics (instantaneous)
	if infra, err := mc.CollectInfrastructureMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect infrastructure metrics")
	} else {
		allMetrics = append(allMetrics, infra...)
	}
	
	// 2. Session metrics (instantaneous + calculated)
	if sessions, err := mc.CollectSessionMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect session metrics")
	} else {
		allMetrics = append(allMetrics, sessions...)
	}
	
	// 3. Logon metrics (calculated on 2min sliding window)
	if logon, err := mc.CollectLogonMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect logon metrics")
	} else {
		allMetrics = append(allMetrics, logon...)
	}
	
	// 4. UX metrics
	if ux, err := mc.CollectUXMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect UX metrics")
	} else {
		allMetrics = append(allMetrics, ux...)
	}
	
	// 5. Failure metrics (calculated on 1h sliding window)
	if failures, err := mc.CollectFailureMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect failure metrics")
	} else {
		allMetrics = append(allMetrics, failures...)
	}
	
	// 6. Health metrics
	if health, err := mc.CollectHealthMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect health metrics")
	} else {
		allMetrics = append(allMetrics, health...)
	}
	
	// Note: Only metric_type tag is preserved for metrics
	
	mc.logger.Info().
		Int("metrics_collected", len(allMetrics)).
		Msg("Complete Citrix metrics collection finished")
	
	return allMetrics, nil
}

// CollectAllMetrics collects and calculates all required metrics using progressive optimized loading
func (mc *MetricsCollector) CollectAllMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Info().Msg("🔍 CITRIX: Starting progressive optimized metrics collection")
	
	// Progressive data loading - load only what we need when we need it
	cache := &CachedDataCollection{
		DesktopGroupMap:      make(map[string]DesktopGroup),
		SessionsByState:      make(map[int][]Session),
		MachinesByController: make(map[string][]Machine),
		CollectionTime:       timestamp,
	}
	
	var allDataPoints []datapoint.DataPoint
	
	// 1. Load Desktop Groups first (essential for all metrics)
	mc.logger.Debug().Msg("Loading desktop groups (required for name resolution)...")
	if dgs, err := mc.client.GetDesktopGroups(ctx); err != nil {
		mc.logger.Error().Err(err).Msg("Failed to get desktop groups - critical for metrics")
		return nil, fmt.Errorf("desktop groups required for metrics: %w", err)
	} else {
		cache.DesktopGroups = dgs
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "DesktopGroups")
		
		// Build lookup map once
		for _, dg := range dgs {
			cache.DesktopGroupMap[dg.GetEffectiveId()] = dg
		}
		mc.logger.Debug().Int("count", len(dgs)).Msg("✅ Desktop groups loaded and mapped")
	}

	// Progressive metrics calculation - load data as needed
	mc.logger.Debug().Msg("Starting progressive metrics calculation")

	// 2. Session Metrics (load sessions when needed)
	mc.logger.Debug().Msg("Loading sessions for session metrics...")
	twentyFourHoursAgo := timestamp.Add(-24 * time.Hour)
	if sessions, err := mc.client.GetSessions(ctx, twentyFourHoursAgo); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get sessions - continuing with partial metrics")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "Sessions")
	} else {
		cache.Sessions = sessions
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "Sessions")
		
		// Pre-group sessions by state (optimization)
		for _, session := range sessions {
			cache.SessionsByState[session.ConnectionState] = append(cache.SessionsByState[session.ConnectionState], session)
		}
		
		// Calculate session metrics immediately
		sessionMetrics := mc.calculateSessionMetrics(timestamp, cache.Sessions, cache.DesktopGroups)
		allDataPoints = append(allDataPoints, sessionMetrics...)
		
		// Add user connection totals
		userConnectionMetrics := mc.calculateUserConnectionTotals(timestamp, cache.Sessions, cache.DesktopGroups)
		allDataPoints = append(allDataPoints, userConnectionMetrics...)
		
		// Add logon duration average for last hour (missing metric!)
		logonDurationMetric := mc.calculateLogonDurationAvgHourly(ctx, timestamp)
		allDataPoints = append(allDataPoints, logonDurationMetric)
		
		mc.logger.Info().
			Int("sessions_loaded", len(sessions)).
			Int("session_metrics", len(sessionMetrics)).
			Float32("logon_duration_avg", logonDurationMetric.Value).
			Msg("✅ Session metrics completed")
	}

	// 3. Machine Metrics (load machines when needed)
	mc.logger.Debug().Msg("Loading machines for machine and infrastructure metrics...")
	if machines, err := mc.client.GetMachines(ctx, time.Time{}); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get machines - continuing with partial metrics")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "Machines")
	} else {
		cache.Machines = machines
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "Machines")
		
		// Pre-group machines by controller (optimization)
		for _, machine := range machines {
			cache.MachinesByController[machine.ControllerDNSName] = append(cache.MachinesByController[machine.ControllerDNSName], machine)
		}
		
		// Calculate machine metrics immediately
		machineMetrics := mc.calculateMachineMetrics(timestamp, cache.Machines, cache.DesktopGroups)
		allDataPoints = append(allDataPoints, machineMetrics...)
		
		// Calculate infrastructure metrics immediately
		infrastructureMetrics := mc.calculateInfrastructureMetricsFromMachines(timestamp, cache.Machines)
		allDataPoints = append(allDataPoints, infrastructureMetrics...)
		
		mc.logger.Info().
			Int("machines_loaded", len(machines)).
			Int("machine_metrics", len(machineMetrics)).
			Int("infrastructure_metrics", len(infrastructureMetrics)).
			Msg("✅ Machine and infrastructure metrics completed")
	}

	// 4. Connection Failures Metrics (load failure data when needed)
	mc.logger.Debug().Msg("Loading connection failures for failure metrics...")
	oneHourAgo := timestamp.Add(-1 * time.Hour)
	
	// Load failure categories first (needed for interpretation)
	if categories, err := mc.client.GetConnectionFailureCategories(ctx); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get failure categories - will use defaults")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "ConnectionFailureCategories")
	} else {
		cache.FailureCategories = categories
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "ConnectionFailureCategories")
	}
	
	// Load failure logs
	if failures, err := mc.client.GetConnectionFailureLogs(ctx, oneHourAgo); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to get connection failures - continuing without failure metrics")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "ConnectionFailureLogs")
	} else {
		cache.ConnectionFailures = failures
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "ConnectionFailureLogs")
		
		// Calculate failure metrics immediately
		if len(failures) > 0 {
			connectionFailuresMetrics := mc.calculateConnectionFailuresMetrics(timestamp, cache.ConnectionFailures, cache.DesktopGroups, cache.FailureCategories)
			allDataPoints = append(allDataPoints, connectionFailuresMetrics...)
			mc.logger.Info().
				Int("failures_loaded", len(failures)).
				Int("failure_metrics", len(connectionFailuresMetrics)).
				Msg("✅ Connection failure metrics completed")
		} else {
			mc.logger.Debug().Msg("No connection failures in last hour - skipping failure metrics")
		}
	}

	// 5. Logon Performance Metrics (2-minute breakdown)
	mc.logger.Debug().Msg("Collecting logon performance metrics...")
	if logonMetrics, err := mc.CollectLogonMetrics(ctx, timestamp); err != nil {
		mc.logger.Warn().Err(err).Msg("Failed to collect logon metrics - continuing without them")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "LogonMetrics")
	} else {
		allDataPoints = append(allDataPoints, logonMetrics...)
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "LogonMetrics")
		mc.logger.Info().
			Int("logon_metrics", len(logonMetrics)).
			Msg("✅ Logon performance metrics completed")
	}

	// Always add a basic health metric showing which endpoints are accessible
	healthMetric := datapoint.DataPoint{
		Name:      "citrix_api_health",
		Value:     float32(len(cache.SuccessfulEndpoints)),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "api_health"},
		},
	}
	allDataPoints = append(allDataPoints, healthMetric)

	mc.logger.Info().
		Strs("successful_endpoints", cache.SuccessfulEndpoints).
		Strs("failed_endpoints", cache.FailedEndpoints).
		Int("total_datapoints", len(allDataPoints)).
		Msg("🎯 Progressive Citrix metrics collection completed - optimized performance")

	return allDataPoints, nil
}

// calculateConnectionFailuresMetrics calculates user connection metrics and failure tracking
func (mc *MetricsCollector) calculateConnectionFailuresMetrics(timestamp time.Time, failures []ConnectionFailureLog, desktopGroups []DesktopGroup, categories []ConnectionFailureCategory) []datapoint.DataPoint {
	mc.logger.Debug().Int("failure_count", len(failures)).Msg("Calculating user connection metrics and failures")
	
	// Build failure code to category mapping
	failureCodeToCategory := make(map[int]int)
	for _, cat := range categories {
		failureCodeToCategory[cat.ConnectionFailureEnumValue] = cat.Category
	}

	var dataPoints []datapoint.DataPoint

	// Track unique users and their failures
	uniqueUsers := make(map[string]bool)
	failuresByType := make(map[int]int)
	failuresByDeliveryGroup := make(map[string]map[int]int)
	failuresByUser := make(map[string]int)
	userFailuresByDeliveryGroup := make(map[string]map[string]int)

	for _, failure := range failures {
		// Track unique users who had failures
		uniqueUsers[failure.UserName] = true
		
		// Map failure code to category
		category := failureCodeToCategory[failure.ConnectionFailureEnumValue]
		if _, exists := failureCategoryNames[category]; !exists {
			// If category is not known, use Other category
			category = FailureCategoryOther
		}
		
		// Count failures by category
		failuresByType[category]++
		
		// Count failures by user
		failuresByUser[failure.UserName]++

		// Count failures by delivery group
		if failuresByDeliveryGroup[failure.DesktopGroupId] == nil {
			failuresByDeliveryGroup[failure.DesktopGroupId] = make(map[int]int)
		}
		failuresByDeliveryGroup[failure.DesktopGroupId][category]++

		// Count user failures by delivery group
		if userFailuresByDeliveryGroup[failure.DesktopGroupId] == nil {
			userFailuresByDeliveryGroup[failure.DesktopGroupId] = make(map[string]int)
		}
		userFailuresByDeliveryGroup[failure.DesktopGroupId][failure.UserName]++
	}

	// 1. Nombre total d'échecs de connexion (fenêtre glissante 1h)
	totalFailures := len(failures)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "user_connection_failures_total",
		Value:     float32(totalFailures),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "user_connections"},
		},
	})

	// 2. Nombre d'utilisateurs uniques avec échecs
	uniqueUsersWithFailures := len(uniqueUsers)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "user_connection_users_with_failures",
		Value:     float32(uniqueUsersWithFailures),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "user_connections"},
		},
	})

	// 3. Échecs par type d'échec (fenêtre glissante)

	for failureType, count := range failuresByType {
		typeName := failureCategoryNames[failureType]
		if typeName == "" {
			typeName = fmt.Sprintf("unknown_type_%d", failureType)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "user_connection_failures_by_type",
			Value:     float32(count),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "user_connections"},
			},
		})
	}

	// 4. Échecs par delivery group avec détail par type
	for dgId, failureTypes := range failuresByDeliveryGroup {
		_ = dgId // Unused after tag removal
		// dgName := mc.getDesktopGroupName(dgId, desktopGroups)
		
		// Total des échecs pour ce delivery group
		totalFailuresForDG := 0
		for _, count := range failureTypes {
			totalFailuresForDG += count
		}
		
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "user_connection_failures_by_delivery_group",
			Value:     float32(totalFailuresForDG),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "user_connections"},
			},
		})
		
		// Détail par type d'échec pour ce delivery group
		for failureType, count := range failureTypes {
			typeName := failureCategoryNames[failureType]
			if typeName == "" {
				typeName = fmt.Sprintf("unknown_type_%d", failureType)
			}

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "user_connection_failures_by_type",
				Value:     float32(count),
				Timestamp: timestamp,
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "user_connections"},
				},
			})
		}
	}

	// 5. Utilisateurs par delivery group avec échecs
	for dgId, userFailures := range userFailuresByDeliveryGroup {
		_ = dgId // Unused after tag removal
		// dgName := mc.getDesktopGroupName(dgId, desktopGroups)
		uniqueUsersForDG := len(userFailures)
		
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "user_connection_users_with_failures",
			Value:     float32(uniqueUsersForDG),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "user_connections"},
			},
		})
	}

	// 6. Métriques legacy pour compatibilité
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "connection_failures_total",
		Value:     float32(totalFailures),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "connection_failures"},
		},
	})

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("User connection metrics calculated")
	return dataPoints
}

// calculateLogonPerformanceMetrics calculates logon performance metrics
func (mc *MetricsCollector) calculateLogonPerformanceMetrics(timestamp time.Time, recentSessions, hourlySessions []Session, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().
		Int("recent_sessions", len(recentSessions)).
		Int("hourly_sessions", len(hourlySessions)).
		Msg("Calculating logon performance metrics")

	var dataPoints []datapoint.DataPoint

	// Filter sessions with valid logon duration from recent sessions
	validRecentSessions := mc.filterSessionsWithLogonDuration(recentSessions)
	validHourlySessions := mc.filterSessionsWithLogonDuration(hourlySessions)

	if len(validRecentSessions) > 0 {
		// Calculate current period statistics
		durations := make([]int, len(validRecentSessions))
		for i, session := range validRecentSessions {
			durations[i] = session.LogOnDuration
		}
		sort.Ints(durations)

		avg := mc.calculateAverage(durations)
		min := durations[0]
		max := durations[len(durations)-1]
		median := mc.calculateMedian(durations)
		p95 := mc.calculatePercentile(durations, 95)

		// Current period metrics
		dataPoints = append(dataPoints, 
			datapoint.DataPoint{
				Name:      "logon_count_current",
				Value:     float32(len(validRecentSessions)),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_average_ms",
				Value:     float32(avg),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_min_ms",
				Value:     float32(min),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_max_ms",
				Value:     float32(max),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_median_ms",
				Value:     float32(median),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_p95_ms",
				Value:     float32(p95),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
		)
	}

	// Calculate 1-hour rolling average
	if len(validHourlySessions) > 0 {
		hourlyDurations := make([]int, len(validHourlySessions))
		for i, session := range validHourlySessions {
			hourlyDurations[i] = session.LogOnDuration
		}

		hourlyAvg := mc.calculateAverage(hourlyDurations)

		dataPoints = append(dataPoints, 
			datapoint.DataPoint{
				Name:      "logon_count_1h_total",
				Value:     float32(len(validHourlySessions)),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_1h_average_ms",
				Value:     float32(hourlyAvg),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			},
		)
	}

	// By delivery group metrics
	logonsByDeliveryGroup := make(map[string][]int)
	for _, session := range validRecentSessions {
		if session.LogOnDuration > 0 {
			logonsByDeliveryGroup[session.DesktopGroupId] = append(logonsByDeliveryGroup[session.DesktopGroupId], session.LogOnDuration)
		}
	}

	for dgId, durations := range logonsByDeliveryGroup {
		if len(durations) > 0 {
			_ = dgId // Unused after tag removal
			// dgName := mc.getDesktopGroupName(dgId, desktopGroups)
			avg := mc.calculateAverage(durations)

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "logon_performance_by_delivery_group",
				Value:     float32(avg),
				Timestamp: timestamp,
				
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
				},
			})
		}
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Logon performance metrics calculated")
	return dataPoints
}

// calculateSessionMetrics calculates active session metrics with advanced filtering
func (mc *MetricsCollector) calculateSessionMetrics(timestamp time.Time, sessions []Session, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().Int("session_count", len(sessions)).Msg("Calculating session metrics")

	var dataPoints []datapoint.DataPoint

	// Count sessions by state globally
	sessionsByState := make(map[int]int)
	sessionsByDeliveryGroup := make(map[string]map[int]int)
	
	// Track unique users for concurrent session counting
	uniqueUsers := make(map[string]bool)
	usersByDeliveryGroup := make(map[string]map[string]bool)

	var sessionsWithEmptyDG int
	var sessionsWithDG int
	
	for _, session := range sessions {
		sessionsByState[session.SessionState]++
		
		// Track unique users globally
		uniqueUsers[session.UserName] = true

		// Count sessions with empty delivery group
		if session.DesktopGroupId == "" {
			sessionsWithEmptyDG++
		} else {
			sessionsWithDG++
		}

		// Initialize delivery group maps if needed
		if sessionsByDeliveryGroup[session.DesktopGroupId] == nil {
			sessionsByDeliveryGroup[session.DesktopGroupId] = make(map[int]int)
		}
		if usersByDeliveryGroup[session.DesktopGroupId] == nil {
			usersByDeliveryGroup[session.DesktopGroupId] = make(map[string]bool)
		}
		
		sessionsByDeliveryGroup[session.DesktopGroupId][session.SessionState]++
		usersByDeliveryGroup[session.DesktopGroupId][session.UserName] = true
	}
	
	mc.logger.Info().
		Int("sessions_with_empty_dg", sessionsWithEmptyDG).
		Int("sessions_with_dg", sessionsWithDG).
		Msg("🔍 CITRIX DELIVERY GROUP DISTRIBUTION")

	// 1. Sessions connectées en live (currently connected)  
	// ONLY include states: 1 (Connected) and 5 (Active)
	// State 0 (Unknown) are NOT active sessions - they are old/orphaned sessions
	connectedSessions := sessionsByState[SessionStateConnected] + sessionsByState[SessionStateActive]
	
	// INFO logging pour diagnostiquer le problème
	mc.logger.Info().
		Int("total_sessions", len(sessions)).
		Int("state_0_unknown", sessionsByState[SessionStateUnknown]).
		Int("state_1_connected", sessionsByState[SessionStateConnected]).
		Int("state_2_disconnected", sessionsByState[SessionStateDisconnected]).
		Int("state_3_terminated", sessionsByState[SessionStateTerminated]).
		Int("state_4_preparing", sessionsByState[SessionStatePreparing]).
		Int("state_5_active", sessionsByState[SessionStateActive]).
		Int("state_6_reconnecting", sessionsByState[SessionStateReconnecting]).
		Int("state_8_other", sessionsByState[SessionStateOther]).
		Int("state_9_pending", sessionsByState[SessionStatePending]).
		Int("calculated_connected", connectedSessions).
		Msg("🔍 CITRIX SESSION STATE BREAKDOWN")
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "sessions_connected",
		Value:     float32(connectedSessions),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	})

	// 2. Sessions simultanées (total concurrent users)
	simultaneousSessions := len(uniqueUsers)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "sessions_simultaneous_users",
		Value:     float32(simultaneousSessions),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	})

	// 3. Sessions déconnectées max (total disconnected)
	disconnectedSessions := sessionsByState[SessionStateDisconnected]
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "sessions_disconnected_max",
		Value:     float32(disconnectedSessions),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	})


	// 6. Métriques par delivery group avec filtrage
	for dgId, stateCount := range sessionsByDeliveryGroup {
		_ = dgId // Unused after tag removal
		// dgName := mc.getDesktopGroupName(dgId, desktopGroups)
		
		// Calculer les totaux pour ce delivery group
		totalForDG := 0
		connectedForDG := 0
		disconnectedForDG := 0
		
		for state, count := range stateCount {
			totalForDG += count
			switch state {
			case SessionStateConnected, SessionStateActive:
				connectedForDG += count
			case SessionStateDisconnected:
				disconnectedForDG += count
			}
		}
		
		// Utilisateurs simultanés pour ce delivery group
		simultaneousUsersForDG := len(usersByDeliveryGroup[dgId])

		// Sessions connectées par delivery group
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "sessions_connected",
			Value:     float32(connectedForDG),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "sessions"},
			},
		})

		// Sessions simultanées par delivery group
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "sessions_simultaneous_users",
			Value:     float32(simultaneousUsersForDG),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "sessions"},
			},
		})

		// Sessions déconnectées max par delivery group
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "sessions_disconnected_max",
			Value:     float32(disconnectedForDG),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "sessions"},
			},
		})

		// Métriques détaillées par état pour compatibilité
		for state, count := range stateCount {
			sessionStateNames := map[int]string{
				SessionStateUnknown:      "unknown",
				SessionStateDisconnected: "disconnected",
				SessionStateConnected:    "connected",
				SessionStateActive:       "active",
			}
			
			stateName := sessionStateNames[state]
			if stateName == "" {
				stateName = fmt.Sprintf("unknown_state_%d", state)
			}

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "sessions_by_state",
				Value:     float32(count),
				Timestamp: timestamp,
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "sessions"},
				},
			})
		}
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Session metrics calculated")
	return dataPoints
}

// calculateUserConnectionTotals calculates total active user connections
func (mc *MetricsCollector) calculateUserConnectionTotals(timestamp time.Time, sessions []Session, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().Int("session_count", len(sessions)).Msg("Calculating user connection totals")

	var dataPoints []datapoint.DataPoint

	// Count active user connections globally and by delivery group
	totalActiveConnections := 0
	activeUsersByDeliveryGroup := make(map[string]map[string]bool)
	
	for _, session := range sessions {
		// Only count connected and active sessions as "current connections"
		if session.SessionState == SessionStateConnected || session.SessionState == SessionStateActive {
			totalActiveConnections++
			
			// Track unique active users per delivery group
			if activeUsersByDeliveryGroup[session.DesktopGroupId] == nil {
				activeUsersByDeliveryGroup[session.DesktopGroupId] = make(map[string]bool)
			}
			activeUsersByDeliveryGroup[session.DesktopGroupId][session.UserName] = true
		}
	}

	// Global total active connections
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "user_connections_total_active",
		Value:     float32(totalActiveConnections),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "user_connections"},
		},
	})

	// Active connections by delivery group
	for dgId, activeUsers := range activeUsersByDeliveryGroup {
		_ = dgId // Unused after tag removal
		// dgName := mc.getDesktopGroupName(dgId, desktopGroups)
		activeConnectionsForDG := len(activeUsers)
		
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "user_connections_total_active",
			Value:     float32(activeConnectionsForDG),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "user_connections"},
			},
		})
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("User connection totals calculated")
	return dataPoints
}

// calculateMachineMetrics calculates machine status metrics with advanced filtering
func (mc *MetricsCollector) calculateMachineMetrics(timestamp time.Time, machines []Machine, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().Int("machine_count", len(machines)).Msg("Calculating machine metrics")

	var dataPoints []datapoint.DataPoint

	// Count machines by registration and fault state globally and by controller
	machinesByRegState := make(map[int]int)
	machinesByFaultState := make(map[int]int)
	machinesByDeliveryGroup := make(map[string]map[string]int)
	machinesByController := make(map[string]map[string]int)
	machinesByControllerAndState := make(map[string]map[int]int)

	// Track unique registration and fault state values for debugging
	uniqueRegStates := make(map[int]bool)
	uniqueFaultStates := make(map[int]bool)

	for _, machine := range machines {
		machinesByRegState[machine.RegistrationState]++
		machinesByFaultState[machine.FaultState]++
		uniqueRegStates[machine.RegistrationState] = true
		uniqueFaultStates[machine.FaultState] = true

		// By delivery group
		if machinesByDeliveryGroup[machine.DesktopGroupId] == nil {
			machinesByDeliveryGroup[machine.DesktopGroupId] = make(map[string]int)
		}
		machinesByDeliveryGroup[machine.DesktopGroupId]["total"]++
		
		if machine.RegistrationState == RegistrationStateRegistered {
			machinesByDeliveryGroup[machine.DesktopGroupId]["registered"]++
		}
		if machine.FaultState != FaultStateNone {
			machinesByDeliveryGroup[machine.DesktopGroupId]["failed"]++
		}

		// By controller with detailed state tracking
		if machine.ControllerDNSName != "" {
			// Initialize controller maps if needed
			if machinesByController[machine.ControllerDNSName] == nil {
				machinesByController[machine.ControllerDNSName] = make(map[string]int)
			}
			if machinesByControllerAndState[machine.ControllerDNSName] == nil {
				machinesByControllerAndState[machine.ControllerDNSName] = make(map[int]int)
			}

			// Total machines per controller
			machinesByController[machine.ControllerDNSName]["total"]++
			
			// Machines by registration state per controller
			machinesByControllerAndState[machine.ControllerDNSName][machine.RegistrationState]++
			
			// Categorize machines by state for controller
			switch machine.RegistrationState {
			case RegistrationStateRegistered:
				machinesByController[machine.ControllerDNSName]["registered"]++
			case RegistrationStateUnregistered:
				machinesByController[machine.ControllerDNSName]["unregistered"]++
			case RegistrationStateAgentError:
				machinesByController[machine.ControllerDNSName]["agent_error"]++
			}
			
			// Categorize by fault state
			if machine.FaultState == FaultStateNone {
				machinesByController[machine.ControllerDNSName]["healthy"]++
			} else {
				machinesByController[machine.ControllerDNSName]["failed"]++
			}
		}
	}

	// Log actual state values found for debugging
	var regStateValues []int
	for state := range uniqueRegStates {
		regStateValues = append(regStateValues, state)
	}
	var faultStateValues []int
	for state := range uniqueFaultStates {
		faultStateValues = append(faultStateValues, state)
	}
	
	mc.logger.Warn().
		Ints("actual_registration_states", regStateValues).
		Ints("actual_fault_states", faultStateValues).
		Msg("DEBUG: Actual machine state values found in Citrix data")

	// Total machines globally
	totalMachines := len(machines)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "machines_total",
		Value:     float32(totalMachines),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "machines"},
		},
	})

	// Machines by registration state globally

	for state, count := range machinesByRegState {
		stateName := registrationStateNames[state]
		if stateName == "" {
			stateName = fmt.Sprintf("unknown_reg_state_%d", state)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "machines_by_state",
			Value:     float32(count),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "machines"},
			},
		})
	}

	// Machines by fault state globally

	for state, count := range machinesByFaultState {
		stateName := faultStateNames[state]
		if stateName == "" {
			stateName = fmt.Sprintf("unknown_fault_state_%d", state)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "machines_by_state",
			Value:     float32(count),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "machines"},
			},
		})
	}

	// Machines by controller DNS name avec filtrage par état
	for _, counts := range machinesByController {
		for _, count := range counts {
			// controllerDNS and stateType unused after tag removal
			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "machines_by_controller",
				Value:     float32(count),
				Timestamp: timestamp,
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "machines"},
				},
			})
		}
	}

	// Machines détaillées par registration state et controller
	for _, stateCount := range machinesByControllerAndState {
		// controllerDNS unused after tag removal
		for state, count := range stateCount {
			stateName := registrationStateNames[state]
			if stateName == "" {
				stateName = fmt.Sprintf("unknown_reg_state_%d", state)
			}

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "machines_by_state",
				Value:     float32(count),
				Timestamp: timestamp,
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "machines"},
				},
			})
		}
	}

	// Machines by delivery group (conservé pour compatibilité)
	for _, counts := range machinesByDeliveryGroup {
		// dgId unused after tag removal
		// dgName := mc.getDesktopGroupName(dgId, desktopGroups)

		for _, count := range counts {
			// metric unused after tag removal
			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "machines_by_delivery_group",
				Value:     float32(count),
				Timestamp: timestamp,
				Tags: []tags.Tag{
					{Key: "metric_type", Value: "machines"},
				},
			})
		}
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Machine metrics calculated")
	return dataPoints
}


// calculateInfrastructureMetricsFromMachines calculates infrastructure status metrics derived from machine data
func (mc *MetricsCollector) calculateInfrastructureMetricsFromMachines(timestamp time.Time, machines []Machine) []datapoint.DataPoint {
	mc.logger.Debug().Int("machine_count", len(machines)).Msg("Calculating infrastructure metrics from machine data")

	var dataPoints []datapoint.DataPoint

	// Group machines by controller to derive controller health
	controllerHealth := make(map[string]struct {
		TotalMachines      int
		RegisteredMachines int
		HealthyMachines    int
		OnlineMachines     int
	})

	for _, machine := range machines {
		if machine.ControllerDNSName != "" {
			health := controllerHealth[machine.ControllerDNSName]
			health.TotalMachines++
			
			if machine.RegistrationState == RegistrationStateRegistered {
				health.RegisteredMachines++
			}
			
			if machine.FaultState == FaultStateNone {
				health.HealthyMachines++
			}
			
			// Consider machine online if it's registered and healthy
			if machine.RegistrationState == RegistrationStateRegistered && machine.FaultState == FaultStateNone {
				health.OnlineMachines++
			}
			
			controllerHealth[machine.ControllerDNSName] = health
		}
	}

	// Generate controller health metrics derived from machine status
	for _, health := range controllerHealth {
		// controllerDNS unused after tag removal
		// Controller perceived online status (1.0 if has registered machines, 0.0 if none)
		var onlineStatus float32 = 0.0
		if health.RegisteredMachines > 0 {
			onlineStatus = 1.0
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "controller_derived_online_status",
			Value:     onlineStatus,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		})

		// Controller health score (percentage of healthy machines)
		var healthScore float32 = 0.0
		if health.TotalMachines > 0 {
			healthScore = float32(health.HealthyMachines) / float32(health.TotalMachines)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "controller_health_score",
			Value:     healthScore,
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		})

		// Machines registered to this controller (actual count from machine data)
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "controller_machines_registered",
			Value:     float32(health.RegisteredMachines),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		})

		// Total machines managed by this controller
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "controller_machines_total",
			Value:     float32(health.TotalMachines),
			Timestamp: timestamp,
			Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
			},
		})
	}

	mc.logger.Debug().
		Int("controllers_found", len(controllerHealth)).
		Int("datapoints_generated", len(dataPoints)).
		Msg("Infrastructure metrics calculated from machine data")
	
	return dataPoints
}

// Helper functions

// filterSessionsWithLogonDuration - DEPRECATED: use helper.FilterSessionsWithLogonDuration instead
func (mc *MetricsCollector) filterSessionsWithLogonDuration(sessions []Session) []Session {
	return mc.helper.FilterSessionsWithLogonDuration(sessions)
}

// getDesktopGroupName gets the desktop group name by ID - DEPRECATED: use helper.GetDesktopGroupName instead
func (mc *MetricsCollector) getDesktopGroupName(dgId string, desktopGroups []DesktopGroup) string {
	// This is a compatibility function - new code should use the cached version
	cache := &CachedDataCollection{
		DesktopGroups: desktopGroups,
		DesktopGroupMap: make(map[string]DesktopGroup),
	}
	
	// Build temporary map for lookup
	for _, dg := range desktopGroups {
		cache.DesktopGroupMap[dg.GetEffectiveId()] = dg
	}
	
	return mc.helper.GetDesktopGroupName(dgId, cache)
}

// calculateAverage - DEPRECATED: use helper.CalculateAverage instead
func (mc *MetricsCollector) calculateAverage(values []int) int {
	return mc.helper.CalculateAverage(values)
}

// calculateMedian - DEPRECATED: use helper.CalculateMedian instead
func (mc *MetricsCollector) calculateMedian(sortedValues []int) int {
	return mc.helper.CalculateMedian(sortedValues)
}

// calculatePercentile - DEPRECATED: use helper.CalculatePercentile instead
func (mc *MetricsCollector) calculatePercentile(sortedValues []int, percentile int) int {
	return mc.helper.CalculatePercentile(sortedValues, percentile)
}