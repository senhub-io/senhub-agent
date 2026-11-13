package configuration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/extension"
	"senhub-agent.go/internal/agent/services/logger"
)

type nopService struct{}

func (nopService) Start(context.Context) error    { return nil }
func (nopService) Shutdown(context.Context) error { return nil }

func claimBlock(t *testing.T, block string) {
	t.Helper()
	extension.ResetForTest()
	t.Cleanup(extension.ResetForTest)
	extension.Register(extension.Extension{
		Name:        "sample",
		ConfigBlock: block,
		New:         func(extension.Host) (extension.Service, error) { return nopService{}, nil },
	})
}

func writeExtensionTree(t *testing.T, root string) string {
	t.Helper()
	writeTestFile(t, root, "agent.yaml", `config_version: 3
agent:
  key: inline-agent-key-0123
sample:
  enabled: true
  endpoint: ${env:EXT_BLOCK_ENDPOINT}
  token: ExtInlineT0ken
  tls:
    insecure: false
  list: [a, b]
unclaimed:
  kept: no
`)
	writeTestFile(t, root, "probes.d/01-a.yaml", "- name: a\n  type: cpu\n")
	return filepath.Join(root, "agent.yaml")
}

func TestClaimedBlockIsLoadedAndSubstituted(t *testing.T) {
	claimBlock(t, "sample")
	t.Setenv("EXT_BLOCK_ENDPOINT", "wss://server.example/v1")
	path := writeExtensionTree(t, t.TempDir())

	data, err := LoadFromDisk(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data.Extensions["unclaimed"]; ok {
		t.Error("a top-level key no extension claims must be dropped")
	}
	block, ok := ExtensionBlockOf(data, "sample")
	if !ok {
		t.Fatalf("claimed block missing: %#v", data.Extensions)
	}
	if block["endpoint"] != "wss://server.example/v1" {
		t.Errorf("endpoint = %v, want the substituted value", block["endpoint"])
	}
	tls, ok := block["tls"].(map[string]interface{})
	if !ok || tls["insecure"] != false {
		t.Errorf("nested block = %#v, want a string-keyed map", block["tls"])
	}

	block["token"] = "mutated"
	again, _ := ExtensionBlockOf(data, "sample")
	if again["token"] != "ExtInlineT0ken" {
		t.Error("ExtensionBlockOf must return a copy the caller cannot use to change the loaded tree")
	}
}

func TestUnclaimedBlocksLeaveTheDataAsBefore(t *testing.T) {
	extension.ResetForTest()
	t.Cleanup(extension.ResetForTest)
	t.Setenv("EXT_BLOCK_ENDPOINT", "wss://server.example/v1")
	data, err := LoadFromDisk(writeExtensionTree(t, t.TempDir()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if data.Extensions != nil {
		t.Errorf("with no extension registered, Extensions = %#v, want nil", data.Extensions)
	}
}

func TestClaimedBlockIsShownRedacted(t *testing.T) {
	claimBlock(t, "sample")
	t.Setenv("EXT_BLOCK_ENDPOINT", "wss://server.example/v1")
	path := writeExtensionTree(t, t.TempDir())

	shown, err := LoadForShow(path, ShowRedact, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := MarshalSortedYAML(&shown)
	if err != nil {
		t.Fatal(err)
	}
	files, err := RedactedConfigFiles(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	var top string
	for _, f := range files {
		if f.Name == "agent.yaml" {
			top = string(f.Body)
		}
	}
	for name, out := range map[string]string{"config show --redact": string(body), "agent.yaml file": top} {
		if !strings.Contains(out, "sample:") || !strings.Contains(out, "wss://server.example/v1") {
			t.Errorf("%s lacks the claimed block:\n%s", name, out)
		}
		if strings.Contains(out, "ExtInlineT0ken") {
			t.Errorf("%s shows the token in clear:\n%s", name, out)
		}
		if strings.Contains(out, "unclaimed") {
			t.Errorf("%s shows an unclaimed key:\n%s", name, out)
		}
	}
}

func TestExtensionBlockChangeIsAReload(t *testing.T) {
	zl := zerolog.Nop()
	lc := &LocalConfiguration{logger: logger.NewModuleLogger((*logger.Logger)(&zl), "configuration.local.test")}
	before := LocalConfigurationData{Extensions: map[string]interface{}{"sample": map[string]interface{}{"enabled": false}}}
	after := LocalConfigurationData{Extensions: map[string]interface{}{"sample": map[string]interface{}{"enabled": true}}}
	if !lc.hasConfigurationChanged(before, after) {
		t.Error("a change confined to an extension block must notify the observers")
	}
	if lc.hasConfigurationChanged(after, after) {
		t.Error("an identical extension block must not be reported as changed")
	}
}
