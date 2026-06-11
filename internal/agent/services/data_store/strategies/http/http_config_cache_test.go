package http

import "testing"

func TestMaxCacheSizeDefault(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	if got := cm.GetMaxCacheSize(); got != DefaultMaxCacheSeries {
		t.Errorf("max_cache_size default: got %d, want %d", got, DefaultMaxCacheSeries)
	}
}

func TestMaxCacheSizeOverride(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  int
	}{
		{"int", 1234, 1234},
		{"int64", int64(2000), 2000},
		{"float64 from JSON/YAML", float64(777), 777},
		{"zero disables the cap", 0, 0},
		{"negative keeps the default", -5, DefaultMaxCacheSeries},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewConfigurationManager(nil, map[string]interface{}{"max_cache_size": tt.value}, newTestLogger())
			if got := cm.GetMaxCacheSize(); got != tt.want {
				t.Errorf("max_cache_size: got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMaxCacheSizeValidation(t *testing.T) {
	cm := NewConfigurationManager(nil, map[string]interface{}{}, newTestLogger())
	tests := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"positive int", 100, false},
		{"zero", 0, false},
		{"negative", -1, true},
		{"non-integer float", 1.5, true},
		{"string", "100", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cm.ValidateConfigParams(map[string]interface{}{"max_cache_size": tt.value})
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error for %v, got nil", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected validation error for %v: %v", tt.value, err)
			}
		})
	}
}
