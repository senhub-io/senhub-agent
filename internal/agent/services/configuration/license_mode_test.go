package configuration

import (
	"os"
	"testing"
)

func TestLicenseSidecarModeFollowsAgentYAMLWithinOwnerAndGroupRead(t *testing.T) {
	cases := []struct{ config, want os.FileMode }{
		{0o640, 0o640},
		{0o600, 0o600},
		{0o644, 0o640},
		{0o660, 0o640},
		{0o400, 0o600},
		{0o777, 0o640},
	}
	for _, c := range cases {
		if got := licenseSidecarMode(c.config); got != c.want {
			t.Errorf("licenseSidecarMode(%o) = %o, want %o", c.config, got, c.want)
		}
	}
}
