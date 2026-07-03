package app

import "testing"

// secretHasFlag decides whether `secret rm` skips its confirmation
// prompt. The arguments passed in are os.Args[2:], i.e. they include
// the sub-verb ("rm") and the positional name.
func TestSecretHasFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{"present after name", []string{"rm", "grafana", "--yes"}, "--yes", true},
		{"present before name", []string{"rm", "--yes", "grafana"}, "--yes", true},
		{"absent", []string{"rm", "grafana"}, "--yes", false},
		{"empty args", nil, "--yes", false},
		{"different flag only", []string{"rm", "grafana", "--config-path", "/x"}, "--yes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secretHasFlag(tc.args, tc.flag); got != tc.want {
				t.Errorf("secretHasFlag(%v, %q) = %v, want %v", tc.args, tc.flag, got, tc.want)
			}
		})
	}
}
