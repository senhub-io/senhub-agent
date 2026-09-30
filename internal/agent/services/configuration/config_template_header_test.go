package configuration

import (
	"strings"
	"testing"
)

// The generated 00-http.yaml opens by naming what it exposes. Prometheus
// joined the default endpoints and the header kept saying PRTG / Nagios /
// Web UI, which is what an operator reads first.
func TestHTTPFragmentHeaderNamesEveryDefaultEndpoint(t *testing.T) {
	header, _, _ := strings.Cut(HTTPStrategyFragmentTemplate, "\n")
	for _, family := range []string{"PRTG", "Nagios", "Prometheus", "Web UI"} {
		if !strings.Contains(header, family) {
			t.Errorf("00-http.yaml header does not name %s: %q", family, header)
		}
	}
}
