package filetail

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestPemBlockOpenBeforeOffsetInBody(t *testing.T) {
	lines := generatePEMLines(t)
	begin := lines[0]
	end := lines[len(lines)-1]
	body := lines[1 : len(lines)-1]

	var buf bytes.Buffer
	buf.WriteString("app start\n")
	buf.Write(begin)
	buf.WriteByte('\n')
	for i := 0; i < 10; i++ {
		buf.Write(body[i])
		buf.WriteByte('\n')
	}
	offset := int64(buf.Len())

	for i := 10; i < len(body); i++ {
		buf.Write(body[i])
		buf.WriteByte('\n')
	}
	buf.Write(end)
	buf.WriteByte('\n')

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected open PEM block before offset")
	}
}

func TestPemBlockOpenBeforeOffsetAfterEnd(t *testing.T) {
	lines := generatePEMLines(t)
	var buf bytes.Buffer
	buf.WriteString("app start\n")
	writePEMLines(&buf, lines)

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("expected no open PEM block after END line")
	}
}

func TestPemBlockOpenBeforeOffsetBeforeBegin(t *testing.T) {
	lines := generatePEMLines(t)
	prefix := []byte("app start\n")

	var buf bytes.Buffer
	buf.Write(prefix)
	writePEMLines(&buf, lines)

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, int64(len(prefix)))
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("expected no open PEM block before BEGIN line")
	}
}

func TestPemBlockOpenBeforeOffsetZero(t *testing.T) {
	lines := generatePEMLines(t)
	var buf bytes.Buffer
	writePEMLines(&buf, lines)

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("expected false for offset 0")
	}
}

func TestPemBlockOpenBeforePreviousClosedThenSecondOpen(t *testing.T) {
	first := generatePEMLines(t)
	second := generatePEMLines(t)

	var buf bytes.Buffer
	writePEMLines(&buf, first)

	begin := second[0]
	end := second[len(second)-1]
	body := second[1 : len(second)-1]

	buf.Write(begin)
	buf.WriteByte('\n')
	for i := 0; i < 10; i++ {
		buf.Write(body[i])
		buf.WriteByte('\n')
	}
	offset := int64(buf.Len())

	for i := 10; i < len(body); i++ {
		buf.Write(body[i])
		buf.WriteByte('\n')
	}
	buf.Write(end)
	buf.WriteByte('\n')

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("expected open PEM block before offset after second BEGIN")
	}
}

func TestPemBlockOpenBeforeBeginOutsideWindow(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("-----BEGIN PRIVATE KEY-----\n")

	line := bytes.Repeat([]byte{'A'}, 64)
	target := 70 << 10
	total := 0
	for total < target {
		buf.Write(line)
		buf.WriteByte('\n')
		total += len(line) + 1
	}

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("expected no open PEM block when BEGIN is outside the window")
	}
}

func TestPemBlockOpenBeforeFileShorterThanOffset(t *testing.T) {
	lines := generatePEMLines(t)
	begin := lines[0]
	body := lines[1 : len(lines)-1]

	var buf bytes.Buffer
	buf.WriteString("app start\n")
	buf.Write(begin)
	buf.WriteByte('\n')
	for i := 0; i < 5; i++ {
		buf.Write(body[i])
		buf.WriteByte('\n')
	}

	path := filepath.Join(t.TempDir(), "key.pem")
	writeTestFile(t, path, buf.Bytes())

	offset := int64(buf.Len() + 100)
	got, err := pemBlockOpenBefore(path, offset)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !got {
		t.Fatal("expected open PEM block before offset")
	}
}

func TestPemBlockOpenBeforeMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.pem")
	_, err := pemBlockOpenBefore(path, 100)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func generatePEMLines(t *testing.T) [][]byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 12 {
		t.Fatalf("unexpectedly short PEM key: %d lines", len(lines))
	}

	return lines
}

func writePEMLines(w *bytes.Buffer, lines [][]byte) {
	for _, line := range lines {
		w.Write(line)
		w.WriteByte('\n')
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPemBlockOpenBeforeBeginAtWindowStart(t *testing.T) {
	var buf bytes.Buffer
	filler := bytes.Repeat([]byte{'x'}, 99)
	for buf.Len() < 10<<10 {
		buf.Write(filler)
		buf.WriteByte('\n')
	}
	start := buf.Len()
	buf.WriteString("-----BEGIN PRIVATE KEY-----\n")
	line := bytes.Repeat([]byte{'A'}, 63)
	for buf.Len() < start+pemResumeWindow {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	buf.Truncate(start + pemResumeWindow)

	path := filepath.Join(t.TempDir(), "edge.log")
	writeTestFile(t, path, buf.Bytes())

	got, err := pemBlockOpenBefore(path, int64(start+pemResumeWindow))
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("BEGIN line at the first byte of the window was not seen")
	}
}
