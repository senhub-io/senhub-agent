// overlay_secrets_test.go
package configuration

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestResolveManagedParams(t *testing.T) {
	originalResolve := resolveSecretFunc
	t.Cleanup(func() { resolveSecretFunc = originalResolve })

	// Stub resolver: knows only "fleet.pg".
	resolveSecretFunc = func(name, dflt string, hasDefault bool) (string, error) {
		if name == "fleet.pg" {
			return "s3cret", nil
		}
		return "", fmt.Errorf("secret not found: %s", name)
	}

	tests := []struct {
		name        string
		input       map[string]any
		want        ProbeConfigParams
		wantErr     bool
		errContains string
		errExcludes string
	}{
		{
			name:  "exact fleet secret resolved",
			input: map[string]any{"password": "${secret:fleet.pg}", "port": 5432},
			want:  ProbeConfigParams{"password": "s3cret", "port": 5432},
		},
		{
			name:    "reject operator-style secret",
			input:   map[string]any{"a": "${secret:db.password}"},
			wantErr: true,
		},
		{
			name:    "reject underscore name",
			input:   map[string]any{"a": "${secret:fleet._agent.keys}"},
			wantErr: true,
		},
		{
			name:    "reject env ref",
			input:   map[string]any{"a": "${env:HOME}"},
			wantErr: true,
		},
		{
			name:    "reject file ref",
			input:   map[string]any{"a": "${file:/etc/shadow}"},
			wantErr: true,
		},
		{
			name:    "reject prefix",
			input:   map[string]any{"a": "x${secret:fleet.pg}"},
			wantErr: true,
		},
		{
			name:    "reject suffix",
			input:   map[string]any{"a": "${secret:fleet.pg}x"},
			wantErr: true,
		},
		{
			name:    "reject nested list",
			input:   map[string]any{"a": []any{"${env:HOME}"}},
			wantErr: true,
		},
		{
			name:    "reject nested map",
			input:   map[string]any{"a": map[string]any{"b": "${secret:db.pw}"}},
			wantErr: true,
		},
		{
			name:        "absent secret error without leakage",
			input:       map[string]any{"a": "${secret:fleet.absent}"},
			wantErr:     true,
			errExcludes: "s3cret",
		},
		{
			name:  "plain string untouched",
			input: map[string]any{"plain": "hello"},
			want:  ProbeConfigParams{"plain": "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveManagedParams(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				if tt.errExcludes != "" && strings.Contains(err.Error(), tt.errExcludes) {
					t.Errorf("error %q must not contain %q", err.Error(), tt.errExcludes)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
