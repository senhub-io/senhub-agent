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

// MetricsCollector handles the collection and calculation of all Citrix metrics
type MetricsCollector struct {
	client CitrixClient
	logger *logger.ModuleLogger
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(client CitrixClient, baseLogger *logger.Logger) *MetricsCollector {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.metrics")
	return &MetricsCollector{
		client: client,
		logger: moduleLogger,
	}
}

// CollectAllMetrics collects and calculates all required metrics
func (mc *MetricsCollector) CollectAllMetrics(ctx context.Context, timestamp time.Time) ([]datapoint.DataPoint, error) {
	mc.logger.Debug().Msg("Starting comprehensive Citrix metrics collection")
	
	var allDataPoints []datapoint.DataPoint

	// Calculate the time ranges for data collection
	twoMinutesAgo := timestamp.Add(-2 * time.Minute)
	oneHourAgo := timestamp.Add(-1 * time.Hour)

	// Collect raw data from APIs concurrently using goroutines
	mc.logger.Debug().Msg("Collecting raw data from Citrix APIs in parallel")
	
	// Use channels and goroutines for parallel API calls
	type apiResult struct {
		name string
		data interface{}
		err  error
	}
	
	resultsChan := make(chan apiResult, 6) // 6 API calls total
	
	// Launch concurrent API calls
	go func() {
		data, err := mc.client.GetSessions(ctx, twoMinutesAgo)
		resultsChan <- apiResult{"recentSessions", data, err}
	}()
	
	go func() {
		data, err := mc.client.GetMachines(ctx, time.Time{}) // Get all machines
		resultsChan <- apiResult{"machines", data, err}
	}()
	
	go func() {
		data, err := mc.client.GetDesktopGroups(ctx)
		resultsChan <- apiResult{"desktopGroups", data, err}
	}()
	
	go func() {
		data, err := mc.client.GetConnectionFailureLogs(ctx, twoMinutesAgo)
		resultsChan <- apiResult{"connectionFailures", data, err}
	}()
	
	go func() {
		data, err := mc.client.GetSessions(ctx, oneHourAgo)
		resultsChan <- apiResult{"hourlySessions", data, err}
	}()
	
	go func() {
		data, err := mc.client.GetControllerStatus(ctx)
		resultsChan <- apiResult{"controllers", data, err}
	}()
	
	// Collect results from all goroutines
	var recentSessions, hourlySessions []Session
	var machines []Machine
	var desktopGroups []DesktopGroup
	var connectionFailures []ConnectionFailureLog
	var controllers []Controller
	
	for i := 0; i < 6; i++ {
		result := <-resultsChan
		
		switch result.name {
		case "recentSessions":
			if result.err != nil {
				mc.logger.Error().Err(result.err).Msg("Failed to get recent sessions")
				return nil, fmt.Errorf("failed to get recent sessions: %v", result.err)
			}
			recentSessions = result.data.([]Session)
			
		case "machines":
			if result.err != nil {
				mc.logger.Error().Err(result.err).Msg("Failed to get machines")
				return nil, fmt.Errorf("failed to get machines: %v", result.err)
			}
			machines = result.data.([]Machine)
			
		case "desktopGroups":
			if result.err != nil {
				mc.logger.Error().Err(result.err).Msg("Failed to get desktop groups")
				return nil, fmt.Errorf("failed to get desktop groups: %v", result.err)
			}
			desktopGroups = result.data.([]DesktopGroup)
			
		case "connectionFailures":
			if result.err != nil {
				mc.logger.Error().Err(result.err).Msg("Failed to get connection failures")
				return nil, fmt.Errorf("failed to get connection failures: %v", result.err)
			}
			connectionFailures = result.data.([]ConnectionFailureLog)
			
		case "hourlySessions":
			if result.err != nil {
				mc.logger.Error().Err(result.err).Msg("Failed to get hourly sessions")
				return nil, fmt.Errorf("failed to get hourly sessions: %v", result.err)
			}
			hourlySessions = result.data.([]Session)
			
		case "controllers":
			if result.err != nil {
				mc.logger.Warn().Err(result.err).Msg("Failed to get controller status (not critical)")
				controllers = []Controller{} // Continue with empty controllers
			} else {
				controllers = result.data.([]Controller)
			}
		}
	}
	
	close(resultsChan)

	mc.logger.Debug().
		Int("recent_sessions", len(recentSessions)).
		Int("machines", len(machines)).
		Int("desktop_groups", len(desktopGroups)).
		Int("connection_failures", len(connectionFailures)).
		Int("hourly_sessions", len(hourlySessions)).
		Int("controllers", len(controllers)).
		Msg("Raw data collection completed")

	// Calculate all metrics
	mc.logger.Debug().Msg("Calculating metrics")

	// 1. Connection Failures Metrics
	connectionFailuresMetrics := mc.calculateConnectionFailuresMetrics(timestamp, connectionFailures, desktopGroups)
	allDataPoints = append(allDataPoints, connectionFailuresMetrics...)

	// 2. Logon Performance Metrics
	logonPerformanceMetrics := mc.calculateLogonPerformanceMetrics(timestamp, recentSessions, hourlySessions, desktopGroups)
	allDataPoints = append(allDataPoints, logonPerformanceMetrics...)

	// 3. Active Sessions Metrics
	sessionMetrics := mc.calculateSessionMetrics(timestamp, recentSessions, desktopGroups)
	allDataPoints = append(allDataPoints, sessionMetrics...)

	// 4. Machine Status Metrics
	machineMetrics := mc.calculateMachineMetrics(timestamp, machines, desktopGroups)
	allDataPoints = append(allDataPoints, machineMetrics...)

	// 5. Infrastructure Status Metrics
	infrastructureMetrics := mc.calculateInfrastructureMetrics(timestamp, controllers)
	allDataPoints = append(allDataPoints, infrastructureMetrics...)

	mc.logger.Info().
		Int("total_datapoints", len(allDataPoints)).
		Msg("Citrix metrics calculation completed")

	return allDataPoints, nil
}

// calculateConnectionFailuresMetrics calculates connection failure metrics
func (mc *MetricsCollector) calculateConnectionFailuresMetrics(timestamp time.Time, failures []ConnectionFailureLog, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().Int("failure_count", len(failures)).Msg("Calculating connection failure metrics")

	var dataPoints []datapoint.DataPoint

	// Count failures by type
	failuresByType := make(map[int]int)
	failuresByDeliveryGroup := make(map[string]map[int]int)

	for _, failure := range failures {
		failuresByType[failure.FailureType]++

		if failuresByDeliveryGroup[failure.DesktopGroupId] == nil {
			failuresByDeliveryGroup[failure.DesktopGroupId] = make(map[int]int)
		}
		failuresByDeliveryGroup[failure.DesktopGroupId][failure.FailureType]++
	}

	// Total failures
	totalFailures := len(failures)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "connection_failures_total",
		Value:     float32(totalFailures),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "connection_failures"},
			{Key: "aggregation", Value: "2min"},
		},
	})

	// Failures by type
	failureTypeNames := map[int]string{
		FailureTypeClientConnection: "client_connection_failures",
		FailureTypeConfiguration:    "configuration_errors",
		FailureTypeMachine:         "machine_failures",
		FailureTypeCapacity:        "unavailable_capacity",
		FailureTypeLicense:         "unavailable_licenses",
	}

	for failureType, count := range failuresByType {
		typeName := failureTypeNames[failureType]
		if typeName == "" {
			typeName = fmt.Sprintf("unknown_type_%d", failureType)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "connection_failures_by_type",
			Value:     float32(count),
			Timestamp: timestamp,
				Tags: []tags.Tag{
				{Key: "metric_type", Value: "connection_failures"},
				{Key: "failure_type", Value: typeName},
				{Key: "aggregation", Value: "2min"},
			},
		})
	}

	// Failures by delivery group
	for dgId, failureTypes := range failuresByDeliveryGroup {
		dgName := mc.getDesktopGroupName(dgId, desktopGroups)
		
		for failureType, count := range failureTypes {
			typeName := failureTypeNames[failureType]
			if typeName == "" {
				typeName = fmt.Sprintf("unknown_type_%d", failureType)
			}

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "connection_failures_by_delivery_group",
				Value:     float32(count),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "connection_failures"},
					{Key: "delivery_group_id", Value: dgId},
					{Key: "delivery_group_name", Value: dgName},
					{Key: "failure_type", Value: typeName},
					{Key: "aggregation", Value: "2min"},
				},
			})
		}
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Connection failure metrics calculated")
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
					{Key: "period", Value: "current_2min"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_average_ms",
				Value:     float32(avg),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "statistic", Value: "average"},
					{Key: "period", Value: "current_2min"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_min_ms",
				Value:     float32(min),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "statistic", Value: "minimum"},
					{Key: "period", Value: "current_2min"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_max_ms",
				Value:     float32(max),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "statistic", Value: "maximum"},
					{Key: "period", Value: "current_2min"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_median_ms",
				Value:     float32(median),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "statistic", Value: "median"},
					{Key: "period", Value: "current_2min"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_p95_ms",
				Value:     float32(p95),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "statistic", Value: "p95"},
					{Key: "period", Value: "current_2min"},
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
					{Key: "period", Value: "1h_rolling"},
				},
			},
			datapoint.DataPoint{
				Name:      "logon_duration_1h_average_ms",
				Value:     float32(hourlyAvg),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "statistic", Value: "average"},
					{Key: "period", Value: "1h_rolling"},
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
			dgName := mc.getDesktopGroupName(dgId, desktopGroups)
			avg := mc.calculateAverage(durations)

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "logon_performance_by_delivery_group",
				Value:     float32(avg),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "logon_performance"},
					{Key: "delivery_group_id", Value: dgId},
					{Key: "delivery_group_name", Value: dgName},
					{Key: "statistic", Value: "average"},
					{Key: "session_count", Value: fmt.Sprintf("%d", len(durations))},
				},
			})
		}
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Logon performance metrics calculated")
	return dataPoints
}

// calculateSessionMetrics calculates active session metrics
func (mc *MetricsCollector) calculateSessionMetrics(timestamp time.Time, sessions []Session, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().Int("session_count", len(sessions)).Msg("Calculating session metrics")

	var dataPoints []datapoint.DataPoint

	// Count sessions by state
	sessionsByState := make(map[int]int)
	sessionsByDeliveryGroup := make(map[string]map[int]int)

	for _, session := range sessions {
		sessionsByState[session.SessionState]++

		if sessionsByDeliveryGroup[session.DesktopGroupId] == nil {
			sessionsByDeliveryGroup[session.DesktopGroupId] = make(map[int]int)
		}
		sessionsByDeliveryGroup[session.DesktopGroupId][session.SessionState]++
	}

	// Total sessions
	totalSessions := len(sessions)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "total_sessions",
		Value:     float32(totalSessions),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "sessions"},
		},
	})

	// Sessions by state
	sessionStateNames := map[int]string{
		SessionStateDisconnected: "disconnected",
		SessionStateConnected:    "connected",
		SessionStateActive:       "active",
	}

	for state, count := range sessionsByState {
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
				{Key: "session_state", Value: stateName},
			},
		})
	}

	// Sessions by delivery group
	for dgId, stateCount := range sessionsByDeliveryGroup {
		dgName := mc.getDesktopGroupName(dgId, desktopGroups)
		totalForDG := 0
		connectedForDG := 0

		for state, count := range stateCount {
			totalForDG += count
			if state == SessionStateConnected || state == SessionStateActive {
				connectedForDG += count
			}
		}

		dataPoints = append(dataPoints, 
			datapoint.DataPoint{
				Name:      "sessions_by_delivery_group_total",
				Value:     float32(totalForDG),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "sessions"},
					{Key: "delivery_group_id", Value: dgId},
					{Key: "delivery_group_name", Value: dgName},
				},
			},
			datapoint.DataPoint{
				Name:      "sessions_by_delivery_group_connected",
				Value:     float32(connectedForDG),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "sessions"},
					{Key: "delivery_group_id", Value: dgId},
					{Key: "delivery_group_name", Value: dgName},
					{Key: "state_filter", Value: "connected_and_active"},
				},
			},
		)
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Session metrics calculated")
	return dataPoints
}

// calculateMachineMetrics calculates machine status metrics
func (mc *MetricsCollector) calculateMachineMetrics(timestamp time.Time, machines []Machine, desktopGroups []DesktopGroup) []datapoint.DataPoint {
	mc.logger.Debug().Int("machine_count", len(machines)).Msg("Calculating machine metrics")

	var dataPoints []datapoint.DataPoint

	// Count machines by registration state
	machinesByRegState := make(map[int]int)
	machinesByFaultState := make(map[int]int)
	machinesByDeliveryGroup := make(map[string]map[string]int)
	machinesByController := make(map[string]int)

	for _, machine := range machines {
		machinesByRegState[machine.RegistrationState]++
		machinesByFaultState[machine.FaultState]++

		// By delivery group
		if machinesByDeliveryGroup[machine.DesktopGroupId] == nil {
			machinesByDeliveryGroup[machine.DesktopGroupId] = make(map[string]int)
		}
		machinesByDeliveryGroup[machine.DesktopGroupId]["total"]++
		
		if machine.RegistrationState == RegistrationStateRegistered {
			machinesByDeliveryGroup[machine.DesktopGroupId]["registered"]++
		}
		if machine.FaultState != FaultStateHealthy {
			machinesByDeliveryGroup[machine.DesktopGroupId]["failed"]++
		}

		// By controller
		if machine.ControllerDNSName != "" {
			machinesByController[machine.ControllerDNSName]++
		}
	}

	// Total machines
	totalMachines := len(machines)
	dataPoints = append(dataPoints, datapoint.DataPoint{
		Name:      "total_machines",
		Value:     float32(totalMachines),
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: "machines"},
		},
	})

	// Machines by registration state
	regStateNames := map[int]string{
		RegistrationStateUnregistered: "unregistered",
		RegistrationStateRegistered:   "registered",
		RegistrationStateAgentError:   "agent_error",
	}

	for state, count := range machinesByRegState {
		stateName := regStateNames[state]
		if stateName == "" {
			stateName = fmt.Sprintf("unknown_reg_state_%d", state)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "machines_by_registration_state",
			Value:     float32(count),
			Timestamp: timestamp,
				Tags: []tags.Tag{
				{Key: "metric_type", Value: "machines"},
				{Key: "registration_state", Value: stateName},
			},
		})
	}

	// Machines by fault state
	faultStateNames := map[int]string{
		FaultStateHealthy: "healthy",
		FaultStateFailed:  "failed",
	}

	for state, count := range machinesByFaultState {
		stateName := faultStateNames[state]
		if stateName == "" {
			stateName = fmt.Sprintf("unknown_fault_state_%d", state)
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "machines_by_fault_state",
			Value:     float32(count),
			Timestamp: timestamp,
				Tags: []tags.Tag{
				{Key: "metric_type", Value: "machines"},
				{Key: "fault_state", Value: stateName},
			},
		})
	}

	// Machines by delivery group
	for dgId, counts := range machinesByDeliveryGroup {
		dgName := mc.getDesktopGroupName(dgId, desktopGroups)

		for metric, count := range counts {
			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "machines_by_delivery_group",
				Value:     float32(count),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "machines"},
					{Key: "delivery_group_id", Value: dgId},
					{Key: "delivery_group_name", Value: dgName},
					{Key: "machine_metric", Value: metric},
				},
			})
		}
	}

	// Machines by controller
	for controllerDNS, count := range machinesByController {
		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "machines_by_controller",
			Value:     float32(count),
			Timestamp: timestamp,
				Tags: []tags.Tag{
				{Key: "metric_type", Value: "machines"},
				{Key: "controller_dns", Value: controllerDNS},
			},
		})
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Machine metrics calculated")
	return dataPoints
}

// calculateInfrastructureMetrics calculates infrastructure status metrics
func (mc *MetricsCollector) calculateInfrastructureMetrics(timestamp time.Time, controllers []Controller) []datapoint.DataPoint {
	mc.logger.Debug().Int("controller_count", len(controllers)).Msg("Calculating infrastructure metrics")

	var dataPoints []datapoint.DataPoint

	// If no controllers data available, return empty metrics
	if len(controllers) == 0 {
		mc.logger.Debug().Msg("No controller data available for infrastructure metrics")
		return dataPoints
	}

	// Controller status metrics
	for _, controller := range controllers {
		// Controller online status
		var onlineStatus float32 = 0.0
		if controller.State == "Online" {
			onlineStatus = 1.0
		}

		dataPoints = append(dataPoints, datapoint.DataPoint{
			Name:      "controller_online_status",
			Value:     onlineStatus,
			Timestamp: timestamp,
				Tags: []tags.Tag{
				{Key: "metric_type", Value: "infrastructure"},
				{Key: "controller_dns", Value: controller.ControllerDNSName},
				{Key: "controller_state", Value: controller.State},
			},
		})

		// Database connection status
		dbStatuses := map[string]string{
			"site_database":          controller.SiteDatabaseStatus,
			"license_server":         controller.LicenseServerStatus,
			"config_logging_database": controller.ConfigLoggingDBStatus,
			"monitoring_database":    controller.MonitoringDBStatus,
		}

		for dbType, status := range dbStatuses {
			var connectedStatus float32 = 0.0
			if status == "Connected" {
				connectedStatus = 1.0
			}

			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "controller_database_status",
				Value:     connectedStatus,
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "infrastructure"},
					{Key: "controller_dns", Value: controller.ControllerDNSName},
					{Key: "database_type", Value: dbType},
					{Key: "connection_status", Value: status},
				},
			})
		}

		// Machines registered to this controller
		if controller.MachinesRegistered > 0 {
			dataPoints = append(dataPoints, datapoint.DataPoint{
				Name:      "controller_machines_registered",
				Value:     float32(controller.MachinesRegistered),
				Timestamp: timestamp,
						Tags: []tags.Tag{
					{Key: "metric_type", Value: "infrastructure"},
					{Key: "controller_dns", Value: controller.ControllerDNSName},
				},
			})
		}
	}

	mc.logger.Debug().Int("datapoints_generated", len(dataPoints)).Msg("Infrastructure metrics calculated")
	return dataPoints
}

// Helper functions

// filterSessionsWithLogonDuration filters sessions that have valid logon duration
func (mc *MetricsCollector) filterSessionsWithLogonDuration(sessions []Session) []Session {
	var filtered []Session
	for _, session := range sessions {
		if session.LogOnDuration > 0 {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

// getDesktopGroupName gets the desktop group name by ID
func (mc *MetricsCollector) getDesktopGroupName(dgId string, desktopGroups []DesktopGroup) string {
	for _, dg := range desktopGroups {
		if dg.DesktopGroupId == dgId {
			return dg.Name
		}
	}
	return "Unknown"
}

// calculateAverage calculates the average of a slice of integers
func (mc *MetricsCollector) calculateAverage(values []int) int {
	if len(values) == 0 {
		return 0
	}
	
	sum := 0
	for _, v := range values {
		sum += v
	}
	return sum / len(values)
}

// calculateMedian calculates the median of a sorted slice of integers
func (mc *MetricsCollector) calculateMedian(sortedValues []int) int {
	length := len(sortedValues)
	if length == 0 {
		return 0
	}
	
	if length%2 == 0 {
		return (sortedValues[length/2-1] + sortedValues[length/2]) / 2
	}
	return sortedValues[length/2]
}

// calculatePercentile calculates the specified percentile of a sorted slice of integers
func (mc *MetricsCollector) calculatePercentile(sortedValues []int, percentile int) int {
	length := len(sortedValues)
	if length == 0 {
		return 0
	}
	
	index := int(float64(length) * float64(percentile) / 100.0)
	if index >= length {
		index = length - 1
	}
	if index < 0 {
		index = 0
	}
	
	return sortedValues[index]
}