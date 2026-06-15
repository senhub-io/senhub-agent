// senhub-agent/internal/agent/services/data_store/http_config_types.go
package http

// Universal Configuration Types

// UniversalConfigRequest represents a universal configuration validation request
type UniversalConfigRequest struct {
	Probe      string                 `json:"probe"`                // Target probe name (e.g., "redfish", "host")
	Target     string                 `json:"target,omitempty"`     // Target system/endpoint URL
	Config     map[string]interface{} `json:"config"`               // Probe-specific configuration
	Validation ConfigValidationMode   `json:"validation,omitempty"` // Validation level to perform
	Timeout    int                    `json:"timeout,omitempty"`    // Timeout for connectivity tests (seconds)
}

// ConfigValidationMode specifies the level of validation to perform
type ConfigValidationMode string

const (
	ValidationSchemaOnly   ConfigValidationMode = "schema"       // Validate structure and types only
	ValidationConnectivity ConfigValidationMode = "connectivity" // + Test network connectivity
	ValidationFull         ConfigValidationMode = "full"         // + Test actual metrics collection
)

// UniversalConfigResponse represents the response from configuration validation
type UniversalConfigResponse struct {
	Valid           bool                            `json:"valid"`                     // Overall validation result
	Probe           string                          `json:"probe"`                     // Probe that was validated
	Target          string                          `json:"target,omitempty"`          // Target that was tested
	ValidationLevel ConfigValidationMode            `json:"validation_level"`          // Level of validation performed
	Tests           map[string]ValidationTestResult `json:"tests"`                     // Individual test results
	Warnings        []string                        `json:"warnings,omitempty"`        // Non-fatal warnings
	Errors          []string                        `json:"errors,omitempty"`          // Validation errors
	PreviewMetrics  []PreviewMetric                 `json:"preview_metrics,omitempty"` // Sample metrics (for full validation)
	Duration        int64                           `json:"duration_ms"`               // Total validation time in milliseconds
}

// ValidationTestResult represents the result of an individual validation test
type ValidationTestResult struct {
	Passed   bool   `json:"passed"`            // Whether this test passed
	Error    string `json:"error,omitempty"`   // Error message if failed
	Duration int64  `json:"duration_ms"`       // Test duration in milliseconds
	Details  string `json:"details,omitempty"` // Additional test details
}

// PreviewMetric represents a sample metric for preview purposes
type PreviewMetric struct {
	Name      string            `json:"name"`      // Metric name
	Value     float32           `json:"value"`     // Metric value
	Tags      map[string]string `json:"tags"`      // Metric tags
	Timestamp int64             `json:"timestamp"` // Collection timestamp
}

// Nagios Configuration Types

// NagiosConfig represents the main Nagios configuration structure
type NagiosConfig struct {
	Version     string        `yaml:"version"`
	Description string        `yaml:"description"`
	Checks      []NagiosCheck `yaml:"checks"`
}

// NagiosCheck represents a single Nagios check definition
type NagiosCheck struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	ProbeFilter string            `yaml:"probe_filter,omitempty"`
	TagFilters  []NagiosTagFilter `yaml:"tag_filters,omitempty"`
	Metrics     []NagiosMetric    `yaml:"metrics"`
}

// NagiosTagFilter represents tag filtering criteria
type NagiosTagFilter struct {
	Key      string   `yaml:"key"`
	Values   []string `yaml:"values,omitempty"`
	Operator string   `yaml:"operator"` // "in", "not_in", "equals", "not_equals", "exists"
}

// NagiosMetric represents a metric definition within a check
type NagiosMetric struct {
	Channel               string               `yaml:"channel"`
	Aggregation           string               `yaml:"aggregation,omitempty"` // "average", "max", "min", "sum", "none"
	Warning               string               `yaml:"warning"`
	Critical              string               `yaml:"critical"`
	Unit                  string               `yaml:"unit,omitempty"`
	Invert                bool                 `yaml:"invert,omitempty"`
	TagContext            string               `yaml:"tag_context,omitempty"`
	TagSpecificThresholds []NagiosTagThreshold `yaml:"tag_specific_thresholds,omitempty"`
	Description           string               `yaml:"description,omitempty"`
}

// NagiosTagThreshold represents tag-specific threshold overrides
type NagiosTagThreshold struct {
	Tags     map[string]string `yaml:"tags"`
	Warning  string            `yaml:"warning"`
	Critical string            `yaml:"critical"`
}

// Nagios Request/Response Types

// NagiosResponse represents the response structure for Nagios checks
type NagiosResponse struct {
	Status     int    `json:"status"`      // 0=OK, 1=WARNING, 2=CRITICAL, 3=UNKNOWN
	StatusText string `json:"status_text"` // "OK", "WARNING", "CRITICAL", "UNKNOWN"
	Message    string `json:"message"`     // Human readable message
	PerfData   string `json:"perfdata"`    // Performance data string
}

// NagiosRequest represents incoming Nagios check requests
type NagiosRequest struct {
	CheckName string                 `json:"check_name,omitempty"`
	Probe     string                 `json:"probe,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Overrides NagiosOverrides        `json:"overrides,omitempty"`
}

// NagiosOverrides represents runtime threshold and filter overrides
type NagiosOverrides struct {
	Warning    string            `json:"warning,omitempty"`
	Critical   string            `json:"critical,omitempty"`
	TagFilters map[string]string `json:"tag_filters,omitempty"`
}

// Nagios Discovery Types

// NagiosCheckInfo represents check information for discovery endpoints
type NagiosCheckInfo struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	ProbeFilter string             `json:"probe_filter,omitempty"`
	MetricCount int                `json:"metric_count"`
	TagFilters  []NagiosTagFilter  `json:"tag_filters,omitempty"`
	Metrics     []NagiosMetricInfo `json:"metrics"`
}

// NagiosMetricInfo represents metric information for discovery
type NagiosMetricInfo struct {
	Channel     string `json:"channel"`
	Aggregation string `json:"aggregation,omitempty"`
	Warning     string `json:"warning"`
	Critical    string `json:"critical"`
	Unit        string `json:"unit,omitempty"`
	Invert      bool   `json:"invert,omitempty"`
	TagContext  string `json:"tag_context,omitempty"`
}
