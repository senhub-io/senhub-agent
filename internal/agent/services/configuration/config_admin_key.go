package configuration

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"senhub-agent.go/internal/agent/services/logger"
)

// EnsureAdminKey gives an existing installation the administration key
// its HTTP output now needs.
//
// The agent key is what a monitoring tool is given to read this agent;
// the administration key is what opens the console, the configuration
// API, the log levels and the cache. An installation made before the
// two were told apart has only the first, so its administration surface
// would not be served at all — including the console its desktop
// shortcut opens.
//
// So one is generated here, on the first start that finds none, written
// into the HTTP fragment in plaintext and sealed into the OS store by
// the pass that runs right after. Idempotent: an installation that has
// the key, under any form including a ${secret:} reference, is left
// alone.
//
// Non-fatal by design, like the other startup migrations: the caller
// logs and carries on. The worst case is a console that answers 404
// until an operator sets the key by hand, which is recoverable; a
// refusal to start is not.
func EnsureAdminKey(configPath string, log *logger.ModuleLogger) error {
	path, doc, httpNode, err := findHTTPOutput(configPath)
	if err != nil || httpNode == nil {
		return err
	}
	if mappingHas(httpNode, "admin_key") {
		return nil
	}

	key, err := newAdminKey()
	if err != nil {
		return err
	}

	raw, err := os.ReadFile(path) // #nosec G304 - the agent's own configuration
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	backup := fmt.Sprintf("%s.backup.%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, raw, 0o600); err != nil {
		return fmt.Errorf("backing up %s before adding the administration key: %w", path, err)
	}

	httpNode.Content = append(httpNode.Content,
		&yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: "admin_key",
			HeadComment: "Opens the console, the configuration API, the log levels and\n" +
				"the cache. The agent key reads; this one changes. Generated\n" +
				"when this agent first needed it.",
		},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key, Style: yaml.DoubleQuotedStyle},
	)

	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("rendering %s with the administration key: %w", path, err)
	}
	if err := atomicWriteFile(path, out, fileModeOr(path, 0o600)); err != nil {
		// Put back exactly what was there rather than leave a half-written
		// output fragment: the agent still starts on the old file.
		if data, e := os.ReadFile(backup); e == nil {
			_ = atomicWriteFile(path, data, fileModeOr(path, 0o600))
		}
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if log != nil {
		log.Info().
			Str("file", filepath.Base(path)).
			Msg("Administration key generated: the console and the configuration API answer it, the agent key no longer opens them")
	}
	return nil
}

// newAdminKey mints the key: a UUID v4, the same shape and the same
// source of randomness as the agent key, so neither is easier to guess
// than the other.
func newAdminKey() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating the administration key: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// findHTTPOutput locates the file holding the http output and returns
// its document, so a caller can edit the mapping in place and re-render
// the file it came from. A configuration with no http output yields a
// nil node and no error: nothing to give a key to.
func findHTTPOutput(configPath string) (string, *yaml.Node, *yaml.Node, error) {
	files, err := sealTargets(configPath, filepath.Dir(configPath))
	if err != nil {
		return "", nil, nil, err
	}
	for _, f := range files {
		raw, err := os.ReadFile(f) // #nosec G304 - the agent's own configuration
		if err != nil {
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			continue
		}
		if node := httpMappingIn(&doc); node != nil {
			return f, &doc, node, nil
		}
	}
	return "", nil, nil, nil
}

// httpMappingIn finds the mapping of the http output, whether it sits at
// the top of a strategies.d fragment or under the storage block of a
// legacy monolithic file.
func httpMappingIn(doc *yaml.Node) *yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if node := mappingValue(root, "http"); node != nil && node.Kind == yaml.MappingNode {
		return node
	}
	if storage := mappingValue(root, "storage"); storage != nil {
		if node := mappingValue(storage, "http"); node != nil && node.Kind == yaml.MappingNode {
			return node
		}
	}
	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.EqualFold(node.Content[i].Value, key) {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingHas(node *yaml.Node, key string) bool {
	return mappingValue(node, key) != nil
}
