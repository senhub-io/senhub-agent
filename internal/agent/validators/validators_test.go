package validators

import "testing"

func TestIsUrl(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "Empty URL",
			url:  "",
			want: false,
		},
		{
			name: "Valid URL",
			url:  "http://example.com",
			want: true,
		},
		{
			name: "Invalid URL",
			url:  "example.com",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsURL(tt.url)
			if got != tt.want {
				t.Errorf("IsURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDuration(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  bool
	}{
		{
			name:  "Float64",
			value: float64(10),
			want:  true,
		},
		{
			name:  "String",
			value: "10s",
			want:  true,
		},
		{
			name:  "Invalid String",
			value: "10",
			want:  false,
		},
		{
			name:  "Invalid Type",
			value: 10,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDuration(tt.value)
			if got != tt.want {
				t.Errorf("IsDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
