package zabbix

import (
	"runtime"
	"strings"
	"testing"
)

func TestTheHostMetadataNamesThePlatformSoTheServerLinksTheRightTemplates(t *testing.T) {
	got := metadataWithPlatform("senhub-agent")
	if !strings.HasPrefix(got, "senhub-agent ") {
		t.Errorf("metadata = %q; what the operator wrote must stay matchable as before", got)
	}
	if !strings.HasSuffix(got, runtime.GOOS) {
		t.Errorf("metadata = %q, want it to end with %s", got, runtime.GOOS)
	}
	if got := metadataWithPlatform("   "); got != runtime.GOOS {
		t.Errorf("empty metadata = %q, want the platform alone", got)
	}
}
