package citrix

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// roundToTwoDecimals rounds a float32 to exactly 2 decimal places
func roundToTwoDecimals(value float32) float32 {
	return float32(math.Round(float64(value)*100) / 100)
}

// CommonMetricsHelper provides shared utilities and caching for metrics calculation
type CommonMetricsHelper struct {
	logger *logger.ModuleLogger

	// Lookup maps for performance
	desktopGroupMap map[string]DesktopGroup
}

// NewCommonMetricsHelper creates a new helper with caching
func NewCommonMetricsHelper(baseLogger *logger.Logger) *CommonMetricsHelper {
	moduleLogger := logger.NewModuleLogger(baseLogger, "probe.citrix.common")
	return &CommonMetricsHelper{
		logger:          moduleLogger,
		desktopGroupMap: make(map[string]DesktopGroup),
	}
}

// CachedDataCollection holds all cached API data for efficient reuse
type CachedDataCollection struct {
	Sessions           []Session
	Machines           []Machine
	DesktopGroups      []DesktopGroup
	ConnectionFailures []ConnectionFailureLog
	FailureCategories  []ConnectionFailureCategory

	// Performance maps
	DesktopGroupMap      map[string]DesktopGroup
	SessionsByState      map[int][]Session
	MachinesByController map[string][]Machine

	CollectionTime      time.Time
	SuccessfulEndpoints []string
	FailedEndpoints     []string
}

// LoadAllDataOnce performs all API calls once and caches results for reuse
func (cmh *CommonMetricsHelper) LoadAllDataOnce(ctx context.Context, client CitrixClient, timestamp time.Time) (*CachedDataCollection, error) {
	cmh.logger.Debug().Msg("🚀 Loading all Citrix data once for optimal performance")

	cache := &CachedDataCollection{
		DesktopGroupMap:      make(map[string]DesktopGroup),
		SessionsByState:      make(map[int][]Session),
		MachinesByController: make(map[string][]Machine),
		CollectionTime:       timestamp,
	}

	// 1. Get Desktop Groups first (required for name resolution)
	cmh.logger.Debug().Msg("Loading desktop groups...")
	if dgs, err := client.GetDesktopGroups(ctx); err != nil {
		cmh.logger.Error().Err(err).Msg("Failed to get desktop groups")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "DesktopGroups")
		return nil, fmt.Errorf("desktop groups required for metrics: %w", err)
	} else {
		cache.DesktopGroups = dgs
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "DesktopGroups")

		// Build desktop group lookup map
		for _, dg := range dgs {
			effectiveId := dg.GetEffectiveId()
			cache.DesktopGroupMap[effectiveId] = dg
		}
		cmh.logger.Debug().Int("count", len(dgs)).Msg("✅ Desktop groups loaded and mapped")
	}

	// 2. Get all Sessions (last 24h for comprehensive data)
	twentyFourHoursAgo := timestamp.Add(-24 * time.Hour)
	cmh.logger.Debug().Msg("Loading sessions (last 24h)...")
	if sessions, err := client.GetSessions(ctx, twentyFourHoursAgo); err != nil {
		cmh.logger.Warn().Err(err).Msg("Failed to get sessions")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "Sessions")
	} else {
		cache.Sessions = sessions
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "Sessions")

		// Pre-group sessions by state for performance
		for _, session := range sessions {
			cache.SessionsByState[session.ConnectionState] = append(cache.SessionsByState[session.ConnectionState], session)
		}
		cmh.logger.Debug().Int("count", len(sessions)).Msg("✅ Sessions loaded and grouped by state")
	}

	// 3. Get all Machines
	cmh.logger.Debug().Msg("Loading machines...")
	if machines, err := client.GetMachines(ctx, time.Time{}); err != nil {
		cmh.logger.Warn().Err(err).Msg("Failed to get machines")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "Machines")
	} else {
		cache.Machines = machines
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "Machines")

		// Pre-group machines by controller for performance
		for _, machine := range machines {
			cache.MachinesByController[machine.ControllerDNSName] = append(cache.MachinesByController[machine.ControllerDNSName], machine)
		}
		cmh.logger.Debug().Int("count", len(machines)).Msg("✅ Machines loaded and grouped by controller")
	}

	// 4. Get Connection Failure Categories (for failure interpretation)
	cmh.logger.Debug().Msg("Loading connection failure categories...")
	if categories, err := client.GetConnectionFailureCategories(ctx); err != nil {
		cmh.logger.Warn().Err(err).Msg("Failed to get failure categories")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "ConnectionFailureCategories")
	} else {
		cache.FailureCategories = categories
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "ConnectionFailureCategories")
		cmh.logger.Debug().Int("count", len(categories)).Msg("✅ Failure categories loaded")
	}

	// 5. Get Connection Failure Logs (last hour)
	oneHourAgo := timestamp.Add(-1 * time.Hour)
	cmh.logger.Debug().Msg("Loading connection failures (last 1h)...")
	if failures, err := client.GetConnectionFailureLogs(ctx, oneHourAgo); err != nil {
		cmh.logger.Warn().Err(err).Msg("Failed to get connection failures")
		cache.FailedEndpoints = append(cache.FailedEndpoints, "ConnectionFailureLogs")
	} else {
		cache.ConnectionFailures = failures
		cache.SuccessfulEndpoints = append(cache.SuccessfulEndpoints, "ConnectionFailureLogs")
		cmh.logger.Debug().Int("count", len(failures)).Msg("✅ Connection failures loaded")
	}

	cmh.logger.Debug().
		Strs("successful", cache.SuccessfulEndpoints).
		Strs("failed", cache.FailedEndpoints).
		Msg("🎯 Data loading completed - ready for efficient metrics calculation")

	return cache, nil
}

// GetDesktopGroupName efficiently resolves desktop group names using cached map
func (cmh *CommonMetricsHelper) GetDesktopGroupName(dgId string, cache *CachedDataCollection) string {
	if dgId == "" {
		return "Unknown (Empty ID)"
	}

	if dg, exists := cache.DesktopGroupMap[dgId]; exists {
		return dg.Name
	}

	return fmt.Sprintf("Unknown (ID: %s)", dgId)
}

// FilterSessionsByTimeWindow filters sessions by start time efficiently
func (cmh *CommonMetricsHelper) FilterSessionsByTimeWindow(sessions []Session, sinceTime time.Time) []Session {
	var filtered []Session
	for _, session := range sessions {
		if session.StartTime.After(sinceTime) {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

// FilterSessionsWithLogonDuration filters sessions with valid logon duration
func (cmh *CommonMetricsHelper) FilterSessionsWithLogonDuration(sessions []Session) []Session {
	var filtered []Session
	for _, session := range sessions {
		if session.LogOnDuration > 0 {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

// CreateBaseDataPoint creates a standardized DataPoint with common tags
func (cmh *CommonMetricsHelper) CreateBaseDataPoint(name string, value float32, timestamp time.Time, metricType string) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name:      name,
		Value:     value,
		Timestamp: timestamp,
		Tags: []tags.Tag{
			{Key: "metric_type", Value: metricType},
		},
	}
}

// Statistical calculation functions (factorized from metrics_collector.go)

// CalculateAverage calculates the average of a slice of integers
func (cmh *CommonMetricsHelper) CalculateAverage(values []int) int {
	if len(values) == 0 {
		return 0
	}

	sum := 0
	for _, v := range values {
		sum += v
	}
	return sum / len(values)
}

// CalculateMedian calculates the median of a slice of integers (will sort internally)
func (cmh *CommonMetricsHelper) CalculateMedian(values []int) int {
	if len(values) == 0 {
		return 0
	}

	// Make a copy to avoid modifying original slice
	sortedValues := make([]int, len(values))
	copy(sortedValues, values)
	sort.Ints(sortedValues)

	length := len(sortedValues)
	if length%2 == 0 {
		return (sortedValues[length/2-1] + sortedValues[length/2]) / 2
	}
	return sortedValues[length/2]
}

// CalculatePercentile calculates the specified percentile of a slice of integers (will sort internally)
func (cmh *CommonMetricsHelper) CalculatePercentile(values []int, percentile int) int {
	if len(values) == 0 {
		return 0
	}

	// Make a copy to avoid modifying original slice
	sortedValues := make([]int, len(values))
	copy(sortedValues, values)
	sort.Ints(sortedValues)

	length := len(sortedValues)
	index := int(float64(length) * float64(percentile) / 100.0)
	if index >= length {
		index = length - 1
	}
	if index < 0 {
		index = 0
	}

	return sortedValues[index]
}

// GetStatistics calculates all common statistics for a set of values
func (cmh *CommonMetricsHelper) GetStatistics(values []int) (avg, min, max, median, p95 int) {
	if len(values) == 0 {
		return 0, 0, 0, 0, 0
	}

	// Sort once for all percentile calculations
	sortedValues := make([]int, len(values))
	copy(sortedValues, values)
	sort.Ints(sortedValues)

	// Calculate all statistics
	avg = cmh.CalculateAverage(values)
	min = sortedValues[0]
	max = sortedValues[len(sortedValues)-1]

	// Median calculation
	length := len(sortedValues)
	if length%2 == 0 {
		median = (sortedValues[length/2-1] + sortedValues[length/2]) / 2
	} else {
		median = sortedValues[length/2]
	}

	// P95 calculation
	index := int(float64(length) * 0.95)
	if index >= length {
		index = length - 1
	}
	p95 = sortedValues[index]

	return avg, min, max, median, p95
}

// GroupSessionsByDeliveryGroup groups sessions by delivery group efficiently
func (cmh *CommonMetricsHelper) GroupSessionsByDeliveryGroup(sessions []Session) map[string][]Session {
	grouped := make(map[string][]Session)
	for _, session := range sessions {
		grouped[session.DesktopGroupId] = append(grouped[session.DesktopGroupId], session)
	}
	return grouped
}

// CountSessionsByState counts sessions by connection state
func (cmh *CommonMetricsHelper) CountSessionsByState(sessions []Session) map[int]int {
	counts := make(map[int]int)
	for _, session := range sessions {
		counts[session.ConnectionState]++
	}
	return counts
}

// CountUniqueUsers counts unique users in a session list
func (cmh *CommonMetricsHelper) CountUniqueUsers(sessions []Session) int {
	uniqueUsers := make(map[string]bool)
	for _, session := range sessions {
		uniqueUsers[session.UserName] = true
	}
	return len(uniqueUsers)
}
