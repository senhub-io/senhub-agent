package snmpmib

import (
	"os"
	"path/filepath"
	"testing"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/logger"
)

func testLogger() *logger.ModuleLogger {
	return logger.NewModuleLogger(logger.NewLogger(&cliArgs.ParsedArgs{}), "test.snmpmib")
}

// A self-contained MIB using only OBJECT IDENTIFIER assignments (no
// SNMPv2-SMI imports needed), under a private enterprise arc so it can't
// collide with the process-global gosmi store.
const testMIB = `TEST-SENHUB-MIB DEFINITIONS ::= BEGIN
testRoot   OBJECT IDENTIFIER ::= { 1 3 6 1 4 1 99991 }
testScalar OBJECT IDENTIFIER ::= { testRoot 1 }
testColumn OBJECT IDENTIFIER ::= { testRoot 2 }
END
`

func TestResolver_LoadsLocalMIBAndResolves(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TEST-SENHUB-MIB"), []byte(testMIB), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Load([]string{dir}, testLogger())

	if name, ok := r.Resolve("1.3.6.1.4.1.99991.1"); !ok || name != "testScalar" {
		t.Errorf("exact node: got %q,%v want testScalar,true", name, ok)
	}
	// Leading dot tolerated.
	if name, ok := r.Resolve(".1.3.6.1.4.1.99991.2"); !ok || name != "testColumn" {
		t.Errorf("leading-dot node: got %q,%v want testColumn,true", name, ok)
	}
	// Instance OID (table-column row index) → nearest ancestor + suffix.
	if name, ok := r.Resolve("1.3.6.1.4.1.99991.2.7"); !ok || name != "testColumn.7" {
		t.Errorf("instance OID: got %q,%v want testColumn.7,true", name, ok)
	}
	// Unrelated OID → miss.
	if name, ok := r.Resolve("1.3.6.1.4.1.88888.1"); ok {
		t.Errorf("unrelated OID should miss, got %q", name)
	}
}

func TestResolver_DisabledWhenNoPaths(t *testing.T) {
	r := Load(nil, testLogger())
	if _, ok := r.Resolve("1.3.6.1.2.1.1.3.0"); ok {
		t.Error("resolver with no MIB paths must always miss")
	}
	// nil resolver is safe too.
	var nilR *Resolver
	if _, ok := nilR.Resolve("1.3.6.1.2.1.1.1.0"); ok {
		t.Error("nil resolver must miss")
	}
}
