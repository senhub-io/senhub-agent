package data_store

import (
	"reflect"
	"testing"
)

// TestUnreadBlockParamsFor exercises the dotted-path descent through
// nested maps, using strategy names of its own so the real
// declarations are untouched.
func TestUnreadBlockParamsFor(t *testing.T) {
	RegisterKnownBlockParams("test-block-strategy", "signals.metrics.tls", []string{"enabled", "ca_file"})
	RegisterKnownBlockParams("test-block-strategy", "retry", []string{"enabled"})

	tests := []struct {
		name   string
		params map[string]interface{}
		want   []string
	}{
		{
			name: "unknown key two levels deep is reported with its full path",
			params: map[string]interface{}{
				"signals": map[string]interface{}{
					"metrics": map[string]interface{}{
						"tls": map[string]interface{}{
							"enabled": true,
							"verfy":   false,
						},
					},
				},
			},
			want: []string{"signals.metrics.tls.verfy"},
		},
		{
			name: "known keys are not reported",
			params: map[string]interface{}{
				"signals": map[string]interface{}{
					"metrics": map[string]interface{}{
						"tls": map[string]interface{}{
							"enabled": true,
							"ca_file": "/etc/ca.pem",
						},
					},
				},
				"retry": map[string]interface{}{"enabled": true},
			},
			want: nil,
		},
		{
			name: "a missing level is ignored",
			params: map[string]interface{}{
				"retry": map[string]interface{}{"enabled": true, "attempts": 3},
			},
			want: []string{"retry.attempts"},
		},
		{
			name: "a non-map level is ignored",
			params: map[string]interface{}{
				"signals": "nope",
				"retry":   map[string]interface{}{"enabled": true},
			},
			want: nil,
		},
		{
			name: "interface-keyed maps are walked",
			params: map[string]interface{}{
				"signals": map[interface{}]interface{}{
					"metrics": map[interface{}]interface{}{
						"tls": map[interface{}]interface{}{
							"enabled": true,
							"keyfile": "/etc/key.pem",
						},
					},
				},
			},
			want: []string{"signals.metrics.tls.keyfile"},
		},
		{
			name:   "an empty configuration reports nothing",
			params: map[string]interface{}{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnreadBlockParamsFor("test-block-strategy", tt.params)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UnreadBlockParamsFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnreadBlockParamsForUndeclaredStrategy pins the silence contract:
// a strategy that declared no blocks reports nothing, same as the
// top-level check.
func TestUnreadBlockParamsForUndeclaredStrategy(t *testing.T) {
	got := UnreadBlockParamsFor("test-block-strategy-other", map[string]interface{}{
		"retry": map[string]interface{}{"anything": true},
	})
	if got != nil {
		t.Errorf("UnreadBlockParamsFor() = %v, want nil for an undeclared strategy", got)
	}
}
