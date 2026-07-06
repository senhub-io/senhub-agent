package app

import "testing"

// db-monitoring only prints SQL / help — it must bypass the privilege
// gate so an operator can draft a monitoring grant without administrator
// on Windows.
func TestReadOnlyCommand_DbMonitoring(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"db-monitoring init", []string{"agent", "db-monitoring", "init", "--engine", "mysql"}, true},
		{"db-monitoring --help", []string{"agent", "db-monitoring", "--help"}, true},
		{"bare db-monitoring", []string{"agent", "db-monitoring"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := readOnlyCommand(tc.args); got != tc.want {
				t.Errorf("readOnlyCommand(%v) = %v, want %v", tc.args[1:], got, tc.want)
			}
		})
	}
}
