package license

import "testing"

func TestVerifyBinding(t *testing.T) {
	const agentA = "60832ce8-ccef-4244-bc88-af3443e8f06c"
	const agentB = "11111111-2222-3333-4444-555555555555"

	cases := []struct {
		name    string
		subject string
		agent   string
		want    bool
	}{
		{"empty subject is valid anywhere", "", agentA, true},
		{"per-agent licence on its own agent", agentA, agentA, true},
		{"per-agent licence on another agent is refused", agentA, agentB, false},
		{"customer licence is valid fleet-wide (agent A)", "client1", agentA, true},
		{"customer licence is valid fleet-wide (agent B)", "client1", agentB, true},
		{"customer id that is not a uuid is never treated as a key", "ACME-2026", agentA, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := VerifyBinding("", c.agent, &License{Subject: c.subject})
			if got != c.want {
				t.Errorf("VerifyBinding(subject=%q, agent=%q) = %v, want %v", c.subject, c.agent, got, c.want)
			}
		})
	}

	if VerifyBinding("", agentA, nil) {
		t.Error("a nil licence must never verify")
	}
}

func TestSubjectIsAgentKey(t *testing.T) {
	if !SubjectIsAgentKey("60832ce8-ccef-4244-bc88-af3443e8f06c") {
		t.Error("a uuid must be recognised as an agent key")
	}
	for _, s := range []string{"", "client1", "ACME-2026", "not-a-uuid", "60832ce8ccef4244bc88af3443e8f06c"} {
		if SubjectIsAgentKey(s) {
			t.Errorf("%q must not be recognised as an agent key", s)
		}
	}
}
