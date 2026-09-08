package outputspec

import (
	"testing"

	"senhub-agent.go/internal/agent/probes/spec"
)

func TestRegisterAndLookup(t *testing.T) {
	Register(Output{Type: "test_out", DisplayName: "Test", Mode: ModePush, Params: []spec.ParamSpec{
		{Key: "endpoint", Kind: spec.KindString, Required: true},
		{Key: "headers", Kind: spec.KindBlock, Fields: []spec.ParamSpec{{Key: "Authorization", Kind: spec.KindString, Secret: true}}},
	}})
	defer func() {
		mu.Lock()
		delete(specs, "test_out")
		mu.Unlock()
	}()
	o, ok := For("test_out")
	if !ok || o.DisplayName != "Test" {
		t.Fatal("registered output not found")
	}
	if problems := o.CheckParams(map[string]interface{}{}); len(problems) != 1 || problems[0].Key != "endpoint" {
		t.Errorf("a missing required key must be reported, got %v", problems)
	}
	if paths := o.SecretPaths(); len(paths) != 1 || paths[0] != "headers.Authorization" {
		t.Errorf("secret paths wrong: %v", paths)
	}
	if _, has := o.DeclaredKeys()["headers.Authorization"]; !has {
		t.Error("nested keys must be declared as block.field")
	}
	found := false
	for _, r := range Registered() {
		if r.Type == "test_out" {
			found = true
		}
	}
	if !found {
		t.Error("Registered must list it")
	}
	defer func() {
		if recover() == nil {
			t.Error("a duplicate registration must panic")
		}
	}()
	Register(Output{Type: "test_out"})
}
