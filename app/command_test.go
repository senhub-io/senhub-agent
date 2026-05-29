package app

import "testing"

// TestIBMICommandSelfRegisters pins that the ibmi verb is wired through
// the command registry (via ibmi.go's init), not the hardcoded dispatch
// switch — the decoupling that lets it move to the enterprise module.
func TestIBMICommandSelfRegisters(t *testing.T) {
	cmd, ok := extraCommands["ibmi"]
	if !ok {
		t.Fatal("ibmi is not registered in extraCommands — its init() should call RegisterCommand")
	}
	if cmd.Run == nil {
		t.Error("registered ibmi command has a nil Run")
	}
	if cmd.ReadOnly {
		t.Error("ibmi is registered ReadOnly=true; it was privilege-gated before the refactor — behaviour changed")
	}
}

func TestRegisterCommandPanics(t *testing.T) {
	cases := []struct {
		name string
		cmd  ExtraCommand
	}{
		{"empty name", ExtraCommand{Name: "", Run: func() {}}},
		{"nil run", ExtraCommand{Name: "totally-new-verb", Run: nil}},
		{"shadows builtin", ExtraCommand{Name: "config", Run: func() {}}},
		{"duplicate", ExtraCommand{Name: "ibmi", Run: func() {}}},
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
