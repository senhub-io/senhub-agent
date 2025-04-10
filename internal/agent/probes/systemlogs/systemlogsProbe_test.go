package systemlogs

import (
	"testing"
	"time"
)

func TestParseSystemLogsProbeConfig(t *testing.T) {
	testCases := []struct {
		name           string
		config         map[string]interface{}
		expectedConfig SystemLogsProbeConfig
		expectError    bool
	}{
		{
			name:   "Default Config",
			config: map[string]interface{}{},
			expectedConfig: SystemLogsProbeConfig{
				MaxEvents: DefaultMaxEvents,
				Interval:  DefaultInterval,
			},
			expectError: false,
		},
		{
			name: "Custom Config with Windows Settings",
			config: map[string]interface{}{
				"sources":    []interface{}{"windowsevents"},
				"max_events": float64(50),
				"interval":   float64(120),
				"windows": map[string]interface{}{
					"channels":  []interface{}{"Application", "Security"},
					"event_ids": []interface{}{float64(1000), float64(1001)},
					"levels":    []interface{}{"Error", "Critical"},
				},
			},
			expectedConfig: SystemLogsProbeConfig{
				Sources:   []LogSource{LogSourceWindowsEvent},
				MaxEvents: 50,
				Interval:  120 * time.Second,
				WindowsSettings: struct {
					Channels []string
					EventIDs []int
					Levels   []string
				}{
					Channels: []string{"Application", "Security"},
					EventIDs: []int{1000, 1001},
					Levels:   []string{"Error", "Critical"},
				},
			},
			expectError: false,
		},
		{
			name: "Custom Config with Journald Settings",
			config: map[string]interface{}{
				"sources":    []interface{}{"journald"},
				"max_events": float64(50),
				"interval":   float64(120),
				"journald": map[string]interface{}{
					"units":    []interface{}{"systemd", "ssh"},
					"priority": []interface{}{"emerg", "alert", "crit"},
				},
			},
			expectedConfig: SystemLogsProbeConfig{
				Sources:   []LogSource{LogSourceJournald},
				MaxEvents: 50,
				Interval:  120 * time.Second,
				JournaldSettings: struct {
					Units    []string
					Priority []string
				}{
					Units:    []string{"systemd", "ssh"},
					Priority: []string{"emerg", "alert", "crit"},
				},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config, err := parseSystemLogsProbeConfig(tc.config)

			if tc.expectError && err == nil {
				t.Fatal("Expected error but got none")
			}

			if !tc.expectError && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.expectError {
				return
			}

			// Check sources
			if len(config.Sources) != len(tc.expectedConfig.Sources) {
				t.Errorf("Expected %d sources, got %d", len(tc.expectedConfig.Sources), len(config.Sources))
			} else {
				for i, src := range tc.expectedConfig.Sources {
					if config.Sources[i] != src {
						t.Errorf("Expected source %s at position %d, got %s", src, i, config.Sources[i])
					}
				}
			}

			// Check Windows settings if applicable
			if len(tc.expectedConfig.WindowsSettings.Channels) > 0 {
				// Check channels
				if len(config.WindowsSettings.Channels) != len(tc.expectedConfig.WindowsSettings.Channels) {
					t.Errorf("Expected %d channels, got %d", len(tc.expectedConfig.WindowsSettings.Channels), len(config.WindowsSettings.Channels))
				} else {
					for i, ch := range tc.expectedConfig.WindowsSettings.Channels {
						if config.WindowsSettings.Channels[i] != ch {
							t.Errorf("Expected channel %s at position %d, got %s", ch, i, config.WindowsSettings.Channels[i])
						}
					}
				}

				// Check EventIDs
				if len(config.WindowsSettings.EventIDs) != len(tc.expectedConfig.WindowsSettings.EventIDs) {
					t.Errorf("Expected %d event IDs, got %d", len(tc.expectedConfig.WindowsSettings.EventIDs), len(config.WindowsSettings.EventIDs))
				} else {
					for i, id := range tc.expectedConfig.WindowsSettings.EventIDs {
						if config.WindowsSettings.EventIDs[i] != id {
							t.Errorf("Expected event ID %d at position %d, got %d", id, i, config.WindowsSettings.EventIDs[i])
						}
					}
				}

				// Check Levels
				if len(config.WindowsSettings.Levels) != len(tc.expectedConfig.WindowsSettings.Levels) {
					t.Errorf("Expected %d levels, got %d", len(tc.expectedConfig.WindowsSettings.Levels), len(config.WindowsSettings.Levels))
				} else {
					for i, level := range tc.expectedConfig.WindowsSettings.Levels {
						if config.WindowsSettings.Levels[i] != level {
							t.Errorf("Expected level %s at position %d, got %s", level, i, config.WindowsSettings.Levels[i])
						}
					}
				}
			}

			// Check Journald settings if applicable
			if len(tc.expectedConfig.JournaldSettings.Units) > 0 {
				// Check units
				if len(config.JournaldSettings.Units) != len(tc.expectedConfig.JournaldSettings.Units) {
					t.Errorf("Expected %d units, got %d", len(tc.expectedConfig.JournaldSettings.Units), len(config.JournaldSettings.Units))
				} else {
					for i, unit := range tc.expectedConfig.JournaldSettings.Units {
						if config.JournaldSettings.Units[i] != unit {
							t.Errorf("Expected unit %s at position %d, got %s", unit, i, config.JournaldSettings.Units[i])
						}
					}
				}

				// Check Priority
				if len(config.JournaldSettings.Priority) != len(tc.expectedConfig.JournaldSettings.Priority) {
					t.Errorf("Expected %d priority levels, got %d", len(tc.expectedConfig.JournaldSettings.Priority), len(config.JournaldSettings.Priority))
				} else {
					for i, prio := range tc.expectedConfig.JournaldSettings.Priority {
						if config.JournaldSettings.Priority[i] != prio {
							t.Errorf("Expected priority %s at position %d, got %s", prio, i, config.JournaldSettings.Priority[i])
						}
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

func TestProcessEvent(t *testing.T) {
	// Create a test event
	testEvent := SystemLogEvent{
		Source:    "TestSource",
		ID:        "12345",
		Level:     "Error",
		Message:   "Test message",
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"custom_field": "custom_value",
		},
	}

	// Create a probe instance
	p := &SystemLogsProbe{
		config: SystemLogsProbeConfig{},
		logger: nil, // We'll ignore logging for this test
	}

	// Process the event
	dataPoint := p.processEvent(testEvent)

	// Verify the resulting DataPoint
	if dataPoint.Name != "systemlogs_event" {
		t.Errorf("Expected name 'systemlogs_event', got '%s'", dataPoint.Name)
	}

	if dataPoint.Value != 1.0 {
		t.Errorf("Expected value 1.0, got %f", dataPoint.Value)
	}

	if !dataPoint.Timestamp.Equal(testEvent.Timestamp) {
		t.Errorf("Expected timestamp %v, got %v", testEvent.Timestamp, dataPoint.Timestamp)
	}

	// Check that required tags are present
	requiredTags := map[string]string{
		"source":  testEvent.Source,
		"id":      testEvent.ID,
		"level":   testEvent.Level,
		"message": testEvent.Message,
	}

	for k, expectedValue := range requiredTags {
		found := false
		for _, tag := range dataPoint.Tags {
			if tag.Key == k {
				if tag.Value != expectedValue {
					t.Errorf("Expected tag %s to have value '%s', got '%s'", k, expectedValue, tag.Value)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Required tag %s not found", k)
		}
	}

	// Check that metadata fields are included as tags
	for k, expectedValue := range testEvent.Metadata {
		found := false
		for _, tag := range dataPoint.Tags {
			if tag.Key == k {
				if tag.Value != expectedValue {
					t.Errorf("Expected metadata tag %s to have value '%s', got '%s'", k, expectedValue, tag.Value)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Metadata tag %s not found", k)
		}
	}
}