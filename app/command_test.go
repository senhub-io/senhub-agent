package app

import "testing"

// TestRegisterCommand verifies a contributed subcommand lands in the
// registry and is dispatchable. Uses a throwaway verb and cleans up the
// process-global registry so it can't bleed into other tests.
func TestRegisterCommand(t *testing.T) {
	const name = "test-verb-register"
	t.Cleanup(func() { delete(extraCommands, name) })

	RegisterCommand(ExtraCommand{Name: name, ReadOnly: true, Run: func() {}})

	cmd, ok := extraCommands[name]
	if !ok {
		t.Fatalf("RegisterCommand(%q) did not populate extraCommands", name)
	}
	if cmd.Run == nil {
		t.Error("registered command has a nil Run")
	}
	if !cmd.ReadOnly {
		t.Error("ReadOnly flag was not preserved")
	}
}

// TestRegisterCommandPanics pins the programmer-error guards: empty
// name, nil Run, a name that shadows a built-in verb, and a duplicate.
func TestRegisterCommandPanics(t *testing.T) {
	const dup = "test-verb-dup"
	RegisterCommand(ExtraCommand{Name: dup, Run: func() {}})
	t.Cleanup(func() { delete(extraCommands, dup) })

	cases := []struct {
		name string
		cmd  ExtraCommand
	}{
		{"empty name", ExtraCommand{Name: "", Run: func() {}}},
		{"nil run", ExtraCommand{Name: "test-verb-nil", Run: nil}},
		{"shadows builtin", ExtraCommand{Name: "config", Run: func() {}}},
		{"duplicate", ExtraCommand{Name: dup, Run: func() {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("RegisterCommand(%+v) did not panic", tc.cmd)
				}
			}()
			RegisterCommand(tc.cmd)
		})
	}
}
