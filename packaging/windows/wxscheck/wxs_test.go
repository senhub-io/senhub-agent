// Package wxscheck guards the WiX sources: WiX rejects a file that is not
// well-formed XML (for instance "--" inside a comment), and that is only
// seen on the Windows runner when the MSI is built at release time.
package wxscheck

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWiXSourcesAreWellFormedXML(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "*.wxs"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no WiX source found next to this package: %v", err)
	}
	for _, f := range files {
		r, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		d := xml.NewDecoder(r)
		for {
			_, err := d.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Errorf("%s: %v", f, err)
				break
			}
		}
		_ = r.Close()
	}
}
