package docscoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The agreement PDFs are rendered outside CI, from a private repository, and
// committed as binaries. The sidecar written by render.py ties each PDF to the
// source and agreement version it came from. CI cannot see that source, so
// this proves only that PDF and sidecar travel together and that the recorded
// PDF hash is the committed file.
func TestLicensePDFsHaveMatchingSidecars(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "docs", "user-guide", "docs", "license")
	pdfs, err := filepath.Glob(filepath.Join(dir, "*.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pdfs) == 0 {
		t.Fatalf("no PDF under %s", dir)
	}
	sidecars, err := filepath.Glob(filepath.Join(dir, "*.pdf.source.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range sidecars {
		if _, err := os.Stat(strings.TrimSuffix(sc, ".source.sha256")); err != nil {
			t.Errorf("%s has no PDF next to it", filepath.Base(sc))
		}
	}

	for _, pdf := range pdfs {
		name := filepath.Base(pdf)
		raw, err := os.ReadFile(pdf + ".source.sha256")
		if err != nil {
			t.Errorf("%s has no sidecar; copy it from adv-commerce next to the PDF: %v", name, err)
			continue
		}
		fields := map[string][]string{}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				fields[parts[0]] = parts[1:]
			}
		}
		if len(fields["agreement_version"]) != 1 {
			t.Errorf("%s sidecar: missing agreement_version", name)
		}
		if src := fields["source"]; len(src) != 2 || len(src[1]) != sha256.Size*2 {
			t.Errorf("%s sidecar: source line must be `source <file> <sha256>`", name)
		}
		recorded := fields["pdf"]
		if len(recorded) != 1 {
			t.Errorf("%s sidecar: missing pdf hash", name)
			continue
		}
		data, err := os.ReadFile(pdf)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != recorded[0] {
			t.Errorf("%s does not match its sidecar (pdf %s, recorded %s); "+
				"regenerate with render.py in adv-commerce and copy PDF and sidecar together",
				name, got, recorded[0])
		}
	}
}
