package template

import (
	"regexp"
	"testing"
)

// An override operation is matched against the resolved name of what the
// rule creates. The pattern must take any value in place of a macro and
// nothing else in place of the literal text.
func TestNameRegexpMatchesTheResolvedNamesOnly(t *testing.T) {
	re := regexp.MustCompile(nameRegexp("{#PROBE}: Disk Used Percent ({#DRIVE}) is above {$SENHUB.DISK_USED_PERCENT.CRIT}%"))
	if !re.MatchString("logicaldisk: Disk Used Percent (C:) is above {$SENHUB.DISK_USED_PERCENT.CRIT}%") {
		t.Error("the pattern misses the resolved trigger name")
	}
	if re.MatchString("logicaldisk: Disk Used Percent (C:) is above {$SENHUB.DISK_USED_PERCENT.WARN}%") {
		t.Error("the pattern also matches the warning trigger")
	}
}
