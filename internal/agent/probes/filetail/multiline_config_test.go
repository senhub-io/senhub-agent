package filetail

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/probes/logparse"
)

func TestParseConfigMultilineFlushTimeout(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		set     bool
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: logparse.DefaultMultilineFlushTimeout},
		{name: "duration", value: "30s", set: true, want: 30 * time.Second},
		{name: "seconds", value: 12, set: true, want: 12 * time.Second},
		{name: "numeric string", value: "2", set: true, want: 2 * time.Second},
		{name: "off", value: 0, set: true, want: 0},
		{name: "negative", value: "-1s", set: true, wantErr: true},
		{name: "not a duration", value: "soon", set: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := map[string]interface{}{"pattern": `^\d`}
			if tt.set {
				block["flush_timeout"] = tt.value
			}
			parsed, err := parseConfig(map[string]interface{}{"paths": []interface{}{"/var/log/app.log"}, "multiline": block})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err == nil && parsed.Multiline.FlushTimeout != tt.want {
				t.Fatalf("FlushTimeout = %s, want %s", parsed.Multiline.FlushTimeout, tt.want)
			}
		})
	}
}
