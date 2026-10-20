package extension

import (
	"context"
	"strings"
	"testing"

	iextension "senhub-agent.go/internal/agent/services/extension"
)

type nop struct{}

func (nop) Start(context.Context) error    { return nil }
func (nop) Shutdown(context.Context) error { return nil }

func TestRegisterReachesTheCoreRegistry(t *testing.T) {
	iextension.ResetForTest()
	t.Cleanup(iextension.ResetForTest)
	Register(Extension{Name: "sdk", ConfigBlock: "sdk_block", New: func(Host) (Service, error) { return nop{}, nil }})
	got := iextension.Registered()
	if len(got) != 1 || got[0].Name != "sdk" || !iextension.ConfigBlocks()["sdk_block"] {
		t.Errorf("Registered = %+v", got)
	}
}

func TestRedactSecrets(t *testing.T) {
	out := RedactSecrets("dial postgres://user:S3cretPw@db:5432/x failed")
	if strings.Contains(out, "S3cretPw") {
		t.Errorf("RedactSecrets left the password: %q", out)
	}
}
