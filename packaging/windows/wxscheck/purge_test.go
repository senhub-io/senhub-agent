package wxscheck

import (
	"os"
	"strings"
	"testing"
)

func readWxs(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../senhub-agent.wxs")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// An uninstall must remove the install folder: Windows Installer leaves any
// directory that still holds a file it did not install, and the agent writes
// there at run time. Every RemoveFolderEx property must always be assigned,
// or Wix4RemoveFoldersEx aborts and takes the sibling purges with it (#707).
func TestInstallFolderIsPurgedOnUninstall(t *testing.T) {
	wxs := readWxs(t)

	for _, want := range []string{
		`<util:RemoveFolderEx Id="PurgeInstallFolder"`,
		`Property="PURGEINSTALLFOLDER"`,
		`<CustomAction Id="SetPurgeInstallDefault" Property="PURGEINSTALLFOLDER" Value="[ProgramFiles64Folder]SenHub Agent\__no_purge__"`,
		`<CustomAction Id="SetPurgeInstallReal"    Property="PURGEINSTALLFOLDER" Value="[INSTALLFOLDER_SAVED]"`,
		`<RegistrySearch Id="FindInstallFolder"`,
		`<Custom Action="SetPurgeInstallDefault" After="SetPurgeUpdateReal" />`,
	} {
		if !strings.Contains(wxs, want) {
			t.Errorf("senhub-agent.wxs does not contain %q", want)
		}
	}

	i := strings.Index(wxs, `<Custom Action="SetPurgeInstallReal"`)
	if i < 0 {
		t.Fatal("SetPurgeInstallReal is not sequenced")
	}
	cond := wxs[i : i+strings.Index(wxs[i:], "/>")]
	for _, gate := range []string{
		`REMOVE~=&quot;ALL&quot;`,
		"NOT UPGRADINGPRODUCTCODE",
		"NOT WIX_UPGRADE_DETECTED",
		"INSTALLFOLDER_SAVED &lt;&gt; &quot;&quot;",
		"INSTALLFOLDER_SAVED &gt;&lt; &quot;SenHub&quot;",
	} {
		if !strings.Contains(cond, gate) {
			t.Errorf("the install folder purge is not gated by %q", gate)
		}
	}
}

// The state folder keeps its own rule: the install folder purge must not
// widen it.
func TestDataFolderPurgeRuleIsUnchanged(t *testing.T) {
	wxs := readWxs(t)
	if !strings.Contains(wxs, `<CustomAction Id="SetPurgeDataReal"      Property="PURGEDATAFOLDER"   Value="[CommonAppDataFolder]SenHub" />`) {
		t.Error("SetPurgeDataReal changed")
	}
}
