package docsparams

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

// Every page carrying a generated parameter block, including the
// commercial pages generated from the enterprise repository whose guard
// never runs here, must still match the digest it was written with. A
// hand edit inside the block of veeam.md used to merge green (#874).
func TestEveryGeneratedParameterBlockIsUnedited(t *testing.T) {
	dir, err := DocsDir()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		raw, err := os.ReadFile(path) // #nosec G304 - walking the repository's own docs tree
		if err != nil {
			return err
		}
		has, ok := VerifyBlock(string(raw))
		if !has {
			return nil
		}
		checked++
		if !ok {
			rel, _ := filepath.Rel(dir, path)
			t.Errorf("%s: the generated parameter block was edited by hand or written without a seal; regenerate it (make docs-params, or the enterprise specguard for a commercial probe)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no generated block found; the guard proves nothing")
	}
}

func TestAHandEditInsideTheBlockBreaksTheSeal(t *testing.T) {
	page := "# Veeam\n\n" + Render(spec.Probe{Type: "veeam", Params: []spec.ParamSpec{{Key: "host", Kind: spec.KindString, Description: "Server"}}}) + "\nprose\n"
	if has, ok := VerifyBlock(page); !has || !ok {
		t.Fatalf("a freshly rendered block does not verify: has=%v ok=%v", has, ok)
	}
	edited := strings.Replace(page, "Server", "Server name", 1)
	if _, ok := VerifyBlock(edited); ok {
		t.Error("an edit inside the block kept the seal valid")
	}
	outside := strings.Replace(page, "prose", "more prose", 1)
	if _, ok := VerifyBlock(outside); !ok {
		t.Error("an edit outside the block broke the seal")
	}
	if _, ok := VerifyBlock(strings.ReplaceAll(page, "\n", "\r\n")); !ok {
		t.Error("a CRLF checkout of an untouched page broke the seal")
	}
}
