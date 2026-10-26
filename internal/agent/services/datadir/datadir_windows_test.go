//go:build windows

package datadir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSetFileSecurityW = windows.NewLazySystemDLL("advapi32.dll").NewProc("SetFileSecurityW")

func TestApplyRemovesUsersACE(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "SenHub")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	b := winBackend{}

	before, err := b.inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before.Protected {
		t.Fatal("a fresh temp directory should inherit its ACL")
	}

	if err := b.apply(dir); err != nil {
		t.Fatal(err)
	}
	after, err := b.inspect(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Protected {
		t.Error("DACL is not protected after apply")
	}
	got := map[string]bool{}
	for _, ace := range after.Allowed {
		got[ace.SID] = true
	}
	if !got["S-1-5-18"] || !got["S-1-5-32-544"] || len(got) != 2 {
		t.Errorf("expected exactly SYSTEM and Administrators, got %v", got)
	}
	if repair, reason := NeedsRepair(after); repair {
		t.Errorf("applied DACL still needs repair: %s", reason)
	}

	if err := os.WriteFile(filepath.Join(dir, "child.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	child, err := b.inspect(filepath.Join(dir, "child.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ace := range child.Allowed {
		if ace.SID == sidBuiltinUsers {
			t.Error("new child file inherited a Users ACE")
		}
	}
}

func sddlDacl(t *testing.T, sddl string) *windows.ACL {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	return dacl
}

func hasSID(s Snapshot, sid string) bool {
	for _, ace := range s.Allowed {
		if strings.EqualFold(ace.SID, sid) {
			return true
		}
	}
	return false
}

func assertInheritedFromRoot(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		t.Errorf("%s DACL is protected, want inherited", path)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount == 0 {
		t.Errorf("%s has missing or empty DACL", path)
		return
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatalf("%s ACE %d: %v", path, i, err)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE == 0 {
			t.Errorf("%s ACE %d is not inherited", path, i)
		}
	}
}

func makeUpgradedTree(t *testing.T) (string, []string) {
	t.Helper()

	root := filepath.Join(t.TempDir(), "SenHub")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}

	usersDacl := sddlDacl(t, "D:(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)")
	if err := windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, usersDacl, nil); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(root, "strategies.d")
	if err := os.Mkdir(subdir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "00-http.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logs"), 0o750); err != nil {
		t.Fatal(err)
	}

	b := winBackend{}
	children := []string{
		filepath.Join(root, "agent.yaml"),
		filepath.Join(root, "logs"),
		filepath.Join(root, "strategies.d"),
		filepath.Join(root, "strategies.d", "00-http.yaml"),
	}
	for _, p := range children {
		snap, err := b.inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		if !hasSID(snap, sidBuiltinUsers) {
			t.Fatalf("child %s did not inherit BU", p)
		}
	}

	// SetFileSecurityW writes the root's descriptor without propagating it to
	// the children, which is what leaves an upgraded tree half-protected.
	// SetSecurityInfo and SetNamedSecurityInfo propagate, so they cannot set
	// up this state.
	sd, err := windows.SecurityDescriptorFromString(ProtectedSDDL)
	if err != nil {
		t.Fatal(err)
	}
	pathp, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, callErr := procSetFileSecurityW.Call(
		uintptr(unsafe.Pointer(pathp)),
		uintptr(windows.DACL_SECURITY_INFORMATION),
		uintptr(unsafe.Pointer(sd)),
	); r == 0 {
		t.Fatalf("SetFileSecurityW: %v", callErr)
	}

	rootSnap, err := b.inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if repair, reason := NeedsRepair(rootSnap); repair {
		t.Fatalf("root should be compliant after root-only protected set: %s", reason)
	}
	for _, p := range children {
		snap, err := b.inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		if !hasSID(snap, sidBuiltinUsers) {
			t.Fatalf("child %s should still carry BU after root-only set", p)
		}
	}

	return root, children
}

func TestTreeRepairAfterUpgrade(t *testing.T) {
	root, children := makeUpgradedTree(t)
	b := winBackend{}

	count, err := ensureTree(b, root, nopLogger())
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected repair count > 0")
	}

	desc, err := b.list(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc) != len(children) {
		t.Fatalf("list returned %d descendants, want %d", len(desc), len(children))
	}

	all := append([]string{root}, desc...)
	for _, p := range all {
		snap, err := b.inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		if broad, sid := grantsBroadAccess(snap); broad {
			t.Errorf("%s still grants broad access to %s", p, sid)
		}
	}

	rootSnap, err := b.inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if repair, reason := NeedsRepair(rootSnap); repair {
		t.Errorf("root still needs repair: %s", reason)
	}
	for _, p := range desc {
		assertInheritedFromRoot(t, p)
	}
}

func TestCleanTreeUntouched(t *testing.T) {
	root, _ := makeUpgradedTree(t)
	b := winBackend{}

	if _, err := ensureTree(b, root, nopLogger()); err != nil {
		t.Fatal(err)
	}

	desc, err := b.list(root)
	if err != nil {
		t.Fatal(err)
	}
	all := append([]string{root}, desc...)

	before := make(map[string]string, len(all))
	for _, p := range all {
		sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		before[p] = sd.String()
	}

	count, err := ensureTree(b, root, nopLogger())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("second ensureTree changed %d paths, want 0", count)
	}
	for _, p := range all {
		sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		if sd.String() != before[p] {
			t.Errorf("SDDL changed for %s", p)
		}
	}
}

func TestListSkipsReparsePoints(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "target.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "regular.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	b := winBackend{}
	got, err := b.list(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only regular file, got %v", got)
	}
	if filepath.Base(got[0]) != "regular.txt" {
		t.Errorf("expected regular.txt, got %v", got)
	}
	for _, p := range got {
		if filepath.Clean(p) == filepath.Clean(link) || strings.HasPrefix(p, filepath.Clean(link)+string(os.PathSeparator)) {
			t.Errorf("symlink was listed: %s", p)
		}
	}
}
