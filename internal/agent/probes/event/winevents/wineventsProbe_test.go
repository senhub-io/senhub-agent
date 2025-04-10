package winevents

import (
	"testing"
	"time"
)

// Mock function for buildWinEventQuery for tests
func buildWinEventQuery(eventIDs []int, levels []string, since time.Time) string {
	// Start with basic query structure
	query := "*"
	
	// Add filters if specified
	if len(eventIDs) > 0 || len(levels) > 0 || !since.IsZero() {
		query = "*[System["
		
		// Add event ID filter
		if len(eventIDs) > 0 {
			if len(eventIDs) == 1 {
				query += "(EventID=" + string(eventIDs[0]) + ")"
			} else {
				query += "("
				for i, id := range eventIDs {
					if i > 0 {
						query += " or "
					}
					query += "EventID=" + string(id)
				}
				query += ")"
			}
		}
		
		// Add level filter
		if len(levels) > 0 {
			if len(eventIDs) > 0 {
				query += " and "
			}
			
			query += "("
			for i, level := range levels {
				if i > 0 {
					query += " or "
				}
				
				// Map level strings to numeric values
				var levelNum string
				switch level {
				case "Critical":
					levelNum = "1"
				case "Error":
					levelNum = "2"
				case "Warning":
					levelNum = "3"
				case "Information":
					levelNum = "4"
				case "Verbose":
					levelNum = "5"
				default:
					continue
				}
				
				query += "Level=" + levelNum
			}
			query += ")"
		}
		
		query += "]]"
	}
	
	return query
}

func TestParseWinEventProbeConfig(t *testing.T) {
	testCases := []struct {
		name           string
		config         map[string]interface{}
		expectedConfig WinEventProbeConfig
		expectError    bool
	}{
		{
			name:   "Default Config",
			config: map[string]interface{}{},
			expectedConfig: WinEventProbeConfig{
				Channels:  []string{"Application", "System"},
				EventIDs:  []int{},
				Levels:    []string{"Critical", "Error", "Warning"},
				MaxEvents: DefaultMaxEvents,
				Interval:  DefaultInterval,
			},
			expectError: false,
		},
		{
			name: "Custom Config",
			config: map[string]interface{}{
				"channels":   []interface{}{"Security", "System"},
				"event_ids":  []interface{}{float64(4624), float64(4625)},
				"levels":     []interface{}{"Error", "Critical"},
				"max_events": float64(50),
				"interval":   float64(120),
			},
			expectedConfig: WinEventProbeConfig{
				Channels:  []string{"Security", "System"},
				EventIDs:  []int{4624, 4625},
				Levels:    []string{"Error", "Critical"},
				MaxEvents: 50,
				Interval:  120 * time.Second,
			},
			expectError: false,
		},
		{
			name: "Too Many Channels",
			config: map[string]interface{}{
				"channels": []interface{}{
					"Channel1", "Channel2", "Channel3", "Channel4", "Channel5", 
					"Channel6", "Channel7", "Channel8", "Channel9", "Channel10", "Channel11",
				},
			},
			expectedConfig: WinEventProbeConfig{},
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config, err := parseWinEventProbeConfig(tc.config)

			if tc.expectError && err == nil {
				t.Fatal("Expected error but got none")
			}

			if !tc.expectError && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.expectError {
				return
			}

			// Check channels
			if len(config.Channels) != len(tc.expectedConfig.Channels) {
				t.Errorf("Expected %d channels, got %d", len(tc.expectedConfig.Channels), len(config.Channels))
			} else {
				for i, ch := range tc.expectedConfig.Channels {
					if config.Channels[i] != ch {
						t.Errorf("Expected channel %s at position %d, got %s", ch, i, config.Channels[i])
					}
				}
			}

			// Check EventIDs
			if len(config.EventIDs) != len(tc.expectedConfig.EventIDs) {
				t.Errorf("Expected %d event IDs, got %d", len(tc.expectedConfig.EventIDs), len(config.EventIDs))
			} else {
				for i, id := range tc.expectedConfig.EventIDs {
					if config.EventIDs[i] != id {
						t.Errorf("Expected event ID %d at position %d, got %d", id, i, config.EventIDs[i])
					}
				}
			}

			// Check Levels
			if len(config.Levels) != len(tc.expectedConfig.Levels) {
				t.Errorf("Expected %d levels, got %d", len(tc.expectedConfig.Levels), len(config.Levels))
			} else {
				for i, level := range tc.expectedConfig.Levels {
					if config.Levels[i] != level {
						t.Errorf("Expected level %s at position %d, got %s", level, i, config.Levels[i])
					}
				}
			}

			// Check MaxEvents
			if config.MaxEvents != tc.expectedConfig.MaxEvents {
				t.Errorf("Expected MaxEvents to be %d, got %d", tc.expectedConfig.MaxEvents, config.MaxEvents)
			}

			// Check Interval
			if config.Interval != tc.expectedConfig.Interval {
				t.Errorf("Expected Interval to be %v, got %v", tc.expectedConfig.Interval, config.Interval)
			}
		})
	}
}

func TestBuildEventQuery(t *testing.T) {
	testCases := []struct {
		name       string
		eventIDs   []int
		levels     []string
		since      time.Time
		expected   string
	}{
		{
			name:       "No Filters",
			eventIDs:   []int{},
			levels:     []string{},
			since:      time.Time{},
			expected:   "*",
		},
		{
			name:       "Event ID Only",
			eventIDs:   []int{4624},
			levels:     []string{},
			since:      time.Time{},
			expected:   "*[System[(EventID=4624)]]",
		},
		{
			name:       "Multiple Event IDs",
			eventIDs:   []int{4624, 4625},
			levels:     []string{},
			since:      time.Time{},
			expected:   "*[System[(EventID=4624 or EventID=4625)]]",
		},
		{
			name:       "Levels Only",
			eventIDs:   []int{},
			levels:     []string{"Error", "Critical"},
			since:      time.Time{},
			expected:   "*[System[(Level=2 or Level=1)]]",
		},
		{
			name:       "Event IDs and Levels",
			eventIDs:   []int{4624},
			levels:     []string{"Error"},
			since:      time.Time{},
			expected:   "*[System[(EventID=4624) and (Level=2)]]",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			query := buildEventQuery(tc.eventIDs, tc.levels, tc.since)
			if query != tc.expected {
				t.Errorf("Expected query '%s', got '%s'", tc.expected, query)
			}
		})
	}
}