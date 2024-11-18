package configParser

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  time.Duration
	}{
		{
			name:  "Float64",
			value: float64(10),
			want:  10 * time.Second,
		},
		{
			name:  "String",
			value: "10s",
			want:  10 * time.Second,
		},
		{
			name:  "Invalid String",
			value: "10",
			want:  0,
		},
		{
			name:  "Invalid Type",
			value: 10,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.value)
			if err != nil {
				if tt.want != 0 {
					t.Errorf("ParseDuration() unexpected error: %v", err)
				}
			} else if got != tt.want {
				t.Errorf("ParseDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
