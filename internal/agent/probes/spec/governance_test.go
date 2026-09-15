package spec

import "testing"

func TestCheckGovernanceReportsUnknownKeysAndShapes(t *testing.T) {
	problems := CheckGovernance(map[string]interface{}{
		"owner":       map[string]interface{}{"team": "sre", "mail": "x"},
		"criticality": "High",
		"labels":      "not-a-map",
		"colour":      "red",
	})
	got := map[string]ProblemKind{}
	for _, p := range problems {
		got[p.Key] = p.Kind
	}
	want := map[string]ProblemKind{
		"governance.owner.mail": ProblemUnknown,
		"governance.labels":     ProblemShape,
		"governance.colour":     ProblemUnknown,
	}
	if len(got) != len(want) {
		t.Fatalf("problems = %v, want %v", problems, want)
	}
	for k, kind := range want {
		if got[k] != kind {
			t.Errorf("%s: kind = %q, want %q (all: %v)", k, got[k], kind, problems)
		}
	}
}

func TestCheckGovernanceAcceptsTheDocumentedShape(t *testing.T) {
	if problems := CheckGovernance(map[string]interface{}{
		"owner":       map[string]interface{}{"team": "devops", "contact": "devops@example.com"},
		"criticality": "high",
		"location":    map[string]interface{}{"site": "paris"},
		"lifecycle":   "active",
		"labels":      map[string]interface{}{"application": "squash-tm"},
	}); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if problems := CheckGovernance("text"); len(problems) != 1 || problems[0].Kind != ProblemShape {
		t.Fatalf("a scalar must be one shape problem, got %v", problems)
	}
	if problems := CheckGovernance(nil); problems != nil {
		t.Fatalf("nil must be silent, got %v", problems)
	}
}
