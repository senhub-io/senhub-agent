package metrics

import (
	"testing"
	
	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
)

// createTestLogger creates a logger for testing
func createTestLogger() *logger.Logger {
	l := zerolog.New(nil)
	return &l
}

func TestMetricCategory_Constants(t *testing.T) {
	tests := []struct {
		name     string
		category MetricCategory
		expected string
	}{
		{"Health category", CategoryHealth, "health"},
		{"Performance category", CategoryPerformance, "performance"},
		{"Capacity category", CategoryCapacity, "capacity"},
		{"Quality category", CategoryQuality, "quality"},
		{"Security category", CategorySecurity, "security"},
		{"Business category", CategoryBusiness, "business"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.category) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.category))
			}
		})
	}
}

func TestMetricSeverity_Constants(t *testing.T) {
	tests := []struct {
		name     string
		severity MetricSeverity
		expected string
	}{
		{"Critical severity", SeverityCritical, "critical"},
		{"High severity", SeverityHigh, "high"},
		{"Medium severity", SeverityMedium, "medium"},
		{"Low severity", SeverityLow, "low"},
		{"Info severity", SeverityInfo, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.severity) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.severity))
			}
		})
	}
}

func TestMetricUnit_Constants(t *testing.T) {
	tests := []struct {
		name     string
		unit     MetricUnit
		expected string
	}{
		{"None unit", UnitNone, ""},
		{"Percent unit", UnitPercent, "percent"},
		{"Boolean unit", UnitBoolean, "boolean"},
		{"Celsius unit", UnitCelsius, "celsius"},
		{"Bytes unit", UnitBytes, "bytes"},
		{"Watts unit", UnitWatts, "watts"},
		{"RPM unit", UnitRPM, "rpm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.unit) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(tt.unit))
			}
		})
	}
}

func TestGetAllCategories(t *testing.T) {
	categories := GetAllCategories()
	
	expectedCategories := []MetricCategory{
		CategoryHealth,
		CategoryPerformance,
		CategoryCapacity,
		CategoryQuality,
		CategorySecurity,
		CategoryBusiness,
	}
	
	if len(categories) != len(expectedCategories) {
		t.Errorf("Expected %d categories, got %d", len(expectedCategories), len(categories))
	}
	
	for i, expected := range expectedCategories {
		if categories[i] != expected {
			t.Errorf("Expected category %s at index %d, got %s", expected, i, categories[i])
		}
	}
}

func TestGetCategorySubcategories(t *testing.T) {
	tests := []struct {
		name               string
		category           MetricCategory
		expectedSubcategories []MetricSubcategory
	}{
		{
			name:     "Health subcategories",
			category: CategoryHealth,
			expectedSubcategories: []MetricSubcategory{
				SubcategoryAvailability,
				SubcategoryConnectivity,
				SubcategoryServiceStatus,
				SubcategorySystemHealth,
			},
		},
		{
			name:     "Performance subcategories",
			category: CategoryPerformance,
			expectedSubcategories: []MetricSubcategory{
				SubcategoryResponseTime,
				SubcategoryThroughput,
				SubcategoryProcessingSpeed,
			},
		},
		{
			name:     "Capacity subcategories",
			category: CategoryCapacity,
			expectedSubcategories: []MetricSubcategory{
				SubcategoryCPU,
				SubcategoryMemory,
				SubcategoryStorage,
				SubcategoryNetwork,
			},
		},
		{
			name:     "Quality subcategories",
			category: CategoryQuality,
			expectedSubcategories: []MetricSubcategory{
				SubcategoryErrorRates,
				SubcategoryStability,
				SubcategoryConsistency,
			},
		},
		{
			name:     "Security subcategories",
			category: CategorySecurity,
			expectedSubcategories: []MetricSubcategory{
				SubcategoryAuthentication,
				SubcategoryAccessControl,
				SubcategoryVulnerability,
			},
		},
		{
			name:     "Business subcategories",
			category: CategoryBusiness,
			expectedSubcategories: []MetricSubcategory{
				SubcategorySLACompliance,
				SubcategoryCost,
				SubcategoryUserImpact,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcategories := GetCategorySubcategories(tt.category)
			
			if len(subcategories) != len(tt.expectedSubcategories) {
				t.Errorf("Expected %d subcategories for %s, got %d", 
					len(tt.expectedSubcategories), tt.category, len(subcategories))
			}
			
			for i, expected := range tt.expectedSubcategories {
				if subcategories[i] != expected {
					t.Errorf("Expected subcategory %s at index %d, got %s", 
						expected, i, subcategories[i])
				}
			}
		})
	}
}

func TestValidateClassification(t *testing.T) {
	tests := []struct {
		name           string
		classification MetricClassification
		expectError    bool
		errorContains  string
	}{
		{
			name: "Valid health/availability classification",
			classification: MetricClassification{
				Category:    CategoryHealth,
				Subcategory: SubcategoryAvailability,
			},
			expectError: false,
		},
		{
			name: "Valid performance/throughput classification",
			classification: MetricClassification{
				Category:    CategoryPerformance,
				Subcategory: SubcategoryThroughput,
			},
			expectError: false,
		},
		{
			name: "Invalid category",
			classification: MetricClassification{
				Category:    MetricCategory("invalid"),
				Subcategory: SubcategoryAvailability,
			},
			expectError:   true,
			errorContains: "invalid category",
		},
		{
			name: "Invalid subcategory for category",
			classification: MetricClassification{
				Category:    CategoryHealth,
				Subcategory: SubcategoryThroughput, // Performance subcategory
			},
			expectError:   true,
			errorContains: "invalid subcategory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClassification(tt.classification)
			
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestClassificationRegistry(t *testing.T) {
	// Create test logger
	testLogger := createTestLogger()
	registry := NewClassificationRegistry(testLogger)
	
	// Create a mock classifier
	mockClassifier := &MockClassifier{
		categories: []MetricCategory{CategoryHealth, CategoryPerformance},
	}
	
	// Test registration
	registry.RegisterClassifier("test_probe", mockClassifier)
	
	// Test retrieval
	classifier, exists := registry.GetClassifier("test_probe")
	if !exists {
		t.Error("Expected classifier to exist after registration")
	}
	if classifier != mockClassifier {
		t.Error("Retrieved classifier does not match registered classifier")
	}
	
	// Test non-existent classifier
	_, exists = registry.GetClassifier("nonexistent")
	if exists {
		t.Error("Expected classifier to not exist")
	}
}

func TestClassificationRegistry_ClassifyMetric(t *testing.T) {
	// Create test logger
	testLogger := createTestLogger()
	registry := NewClassificationRegistry(testLogger)
	
	// Register mock classifier
	mockClassifier := &MockClassifier{
		classification: MetricClassification{
			Category:    CategoryHealth,
			Subcategory: SubcategorySystemHealth,
			Severity:    SeverityHigh,
			Unit:        UnitBoolean,
		},
	}
	registry.RegisterClassifier("test_probe", mockClassifier)
	
	// Test classification with registered probe
	classification, err := registry.ClassifyMetric("test_probe", "test_metric", 1.0, []tags.Tag{})
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if classification.Category != CategoryHealth {
		t.Errorf("Expected category %s, got %s", CategoryHealth, classification.Category)
	}
	
	// Test classification with unregistered probe (should return default)
	classification, err = registry.ClassifyMetric("unknown_probe", "test_metric", 1.0, []tags.Tag{})
	if err != nil {
		t.Errorf("Expected no error for unknown probe, got: %v", err)
	}
	if classification.Category != CategoryHealth {
		t.Errorf("Expected default category %s, got %s", CategoryHealth, classification.Category)
	}
}

func TestThresholdRange(t *testing.T) {
	// Test creating threshold ranges
	min := 10.0
	max := 90.0
	
	threshold := &ThresholdRange{
		Min: &min,
		Max: &max,
	}
	
	if *threshold.Min != 10.0 {
		t.Errorf("Expected min 10.0, got %f", *threshold.Min)
	}
	if *threshold.Max != 90.0 {
		t.Errorf("Expected max 90.0, got %f", *threshold.Max)
	}
	
	// Test nil values
	threshold = &ThresholdRange{}
	if threshold.Min != nil {
		t.Error("Expected Min to be nil")
	}
	if threshold.Max != nil {
		t.Error("Expected Max to be nil")
	}
}

// MockClassifier implements MetricClassifier for testing
type MockClassifier struct {
	categories     []MetricCategory
	classification MetricClassification
}

func (m *MockClassifier) ClassifyMetric(metricName string, value float64, tags []tags.Tag) MetricClassification {
	return m.classification
}

func (m *MockClassifier) GetSupportedCategories() []MetricCategory {
	return m.categories
}

func (m *MockClassifier) GetCategoryMetrics(category MetricCategory) []string {
	return []string{"mock_metric_1", "mock_metric_2"}
}

// Helper function to check if a string contains a substring
func contains(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || (len(substr) > 0 && containsHelper(str, substr)))
}

func containsHelper(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}