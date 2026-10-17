package yamlcompat

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Unmarshal decodes YAML with yaml.v3 while preserving the yaml.v2 semantics
// for duplicate mapping keys, YAML 1.1 boolean words and timestamps inside
// interface{} destinations, and quoted scalars decoded into bool destinations.
func Unmarshal(data []byte, out interface{}) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Kind == 0 {
		return nil
	}

	var collected []string
	if out != nil {
		rewriteTypes(&doc, reflect.TypeOf(out), map[visitKey]bool{}, &collected)
	}
	// After the rewrite: `yes` and `on` become the same boolean key.
	deduplicateMappingKeys(&doc)

	decodeErr := doc.Decode(out)
	if len(collected) == 0 {
		return decodeErr
	}

	errs := make([]string, 0, len(collected)+1)
	if decodeErr != nil {
		var te *yaml.TypeError
		if errors.As(decodeErr, &te) {
			errs = append(errs, te.Errors...)
		} else {
			errs = append(errs, decodeErr.Error())
		}
	}
	errs = append(errs, collected...)
	return &yaml.TypeError{Errors: errs}
}

// Marshal encodes v with the two-space indent yaml.v2 used, where yaml.v3
// defaults to four. Sequences nested in a mapping are indented under their
// key, which yaml.v2 did not do and yaml.v3 offers no switch to undo; a
// reader sees the same document either way.
func Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// v2 keeps the last duplicate key silently; v3 refuses duplicate keys.
// Clear earlier duplicates before decoding.
func deduplicateMappingKeys(doc *yaml.Node) {
	walkDedup(doc, map[*yaml.Node]bool{})
}

func walkDedup(n *yaml.Node, seen map[*yaml.Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true

	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			walkDedup(c, seen)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			walkDedup(c, seen)
		}
	case yaml.MappingNode:
		deduplicateMapping(n)
		for _, c := range n.Content {
			walkDedup(c, seen)
		}
	}
}

func deduplicateMapping(n *yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode || len(n.Content)%2 != 0 {
		return
	}

	type pair struct {
		key   *yaml.Node
		value *yaml.Node
	}
	pairs := make([]pair, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		pairs = append(pairs, pair{key: n.Content[i], value: n.Content[i+1]})
	}
	if len(pairs) == 0 {
		return
	}

	keep := make([]bool, len(pairs))
	for i := range keep {
		keep[i] = true
	}
	for i := 0; i < len(pairs); i++ {
		if !keep[i] {
			continue
		}
		for j := i + 1; j < len(pairs); j++ {
			if !keep[j] {
				continue
			}
			if keysEqual(pairs[i].key, pairs[j].key) {
				keep[i] = false
				break
			}
		}
	}

	content := make([]*yaml.Node, 0, len(pairs)*2)
	for i, p := range pairs {
		if keep[i] {
			content = append(content, p.key, p.value)
		}
	}
	n.Content = content
}

func keysEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil || a.Kind != yaml.ScalarNode || b.Kind != yaml.ScalarNode {
		return false
	}
	if isMergeKey(a) || isMergeKey(b) {
		return false
	}
	at, bt := a.ShortTag(), b.ShortTag()
	if at == "!!str" && bt == "!!str" {
		return a.Value == b.Value
	}
	return at == bt && a.Value == b.Value
}

func isMergeKey(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!merge"
}

type visitKey struct {
	node *yaml.Node
	typ  reflect.Type
}

func rewriteTypes(n *yaml.Node, t reflect.Type, visited map[visitKey]bool, collected *[]string) {
	if n == nil || t == nil {
		return
	}

	for t.Kind() == reflect.Ptr {
		if typeStops(t) {
			return
		}
		t = t.Elem()
	}
	if typeStops(t) {
		return
	}

	key := visitKey{node: n, typ: t}
	if visited[key] {
		return
	}
	visited[key] = true

	if n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			rewriteTypes(c, t, visited, collected)
		}
		return
	}
	if n.Kind == yaml.AliasNode {
		rewriteTypes(n.Alias, t, visited, collected)
		return
	}

	if isEmptyInterface(t) {
		rewriteAnySubtree(n, visited)
		return
	}

	switch t.Kind() {
	case reflect.Struct:
		switch n.Kind {
		case yaml.MappingNode:
			rewriteStructMapping(n, t, visited, collected)
		case yaml.SequenceNode:
			// Merge values may contain a sequence of mappings or aliases.
			for _, item := range n.Content {
				rewriteTypes(item, t, visited, collected)
			}
		}
	case reflect.Map:
		switch n.Kind {
		case yaml.MappingNode:
			rewriteMapMapping(n, t, visited, collected)
		case yaml.SequenceNode:
			for _, item := range n.Content {
				rewriteTypes(item, t, visited, collected)
			}
		}
	case reflect.Slice, reflect.Array:
		if n.Kind == yaml.SequenceNode {
			elem := t.Elem()
			for _, item := range n.Content {
				rewriteTypes(item, elem, visited, collected)
			}
		}
	case reflect.Bool:
		if n.Kind == yaml.ScalarNode {
			if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
				*collected = append(*collected, fmt.Sprintf("line %d: cannot unmarshal !!str `%s` into bool", n.Line, n.Value))
			}
		}
	}
}

func rewriteAnySubtree(n *yaml.Node, visited map[visitKey]bool) {
	if n == nil {
		return
	}
	key := visitKey{node: n}
	if visited[key] {
		return
	}
	visited[key] = true

	switch n.Kind {
	case yaml.ScalarNode:
		rewriteAnyScalar(n)
	case yaml.AliasNode:
		rewriteAnySubtree(n.Alias, visited)
	case yaml.DocumentNode:
		for _, c := range n.Content {
			rewriteAnySubtree(c, visited)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			rewriteAnySubtree(c, visited)
		}
	case yaml.MappingNode:
		for _, c := range n.Content {
			rewriteAnySubtree(c, visited)
		}
	}
}

func rewriteAnyScalar(n *yaml.Node) {
	if n.Kind != yaml.ScalarNode {
		return
	}

	// v2 treats these YAML 1.1 words as booleans in interface{} destinations;
	// v3 treats them as strings there.
	if n.ShortTag() == "!!str" && isPlainScalar(n) {
		if value, ok := boolWordValue(n.Value); ok {
			n.Tag = "!!bool"
			n.Value = value
			return
		}
	}

	// v2 leaves plain timestamps as strings in interface{} destinations;
	// v3 decodes them as time.Time.
	if n.ShortTag() == "!!timestamp" && isPlainScalar(n) {
		n.Tag = "!!str"
	}
}

func boolWordValue(s string) (string, bool) {
	switch s {
	case "y", "Y", "yes", "Yes", "YES", "on", "On", "ON":
		return "true", true
	case "n", "N", "no", "No", "NO", "off", "Off", "OFF":
		return "false", true
	default:
		return "", false
	}
}

func isPlainScalar(n *yaml.Node) bool {
	return n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) == 0
}

func rewriteStructMapping(n *yaml.Node, st reflect.Type, visited map[visitKey]bool, collected *[]string) {
	if n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if isMergeKey(key) {
			rewriteTypes(val, st, visited, collected)
			continue
		}
		if key.Kind != yaml.ScalarNode {
			continue
		}
		if ft, ok := lookupStructField(st, key.Value); ok {
			rewriteTypes(val, ft, visited, collected)
		}
	}
}

func rewriteMapMapping(n *yaml.Node, mt reflect.Type, visited map[visitKey]bool, collected *[]string) {
	if n.Kind != yaml.MappingNode {
		return
	}
	elem := mt.Elem()
	keyType := mt.Key()
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if isMergeKey(key) {
			rewriteTypes(val, mt, visited, collected)
			continue
		}
		if isEmptyInterface(keyType) {
			rewriteTypes(key, keyType, visited, collected)
		} else if keyType.Kind() == reflect.String && key.Kind == yaml.ScalarNode && key.ShortTag() == "!!null" && isPlainScalar(key) {
			// v2 stores a null key in a string-keyed map as "", v3 drops the entry.
			key.Tag = "!!str"
			key.Value = ""
		}
		rewriteTypes(val, elem, visited, collected)
	}
}

func lookupStructField(st reflect.Type, key string) (reflect.Type, bool) {
	// Regular fields first.
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tagIgnored(tag) {
			continue
		}
		name, inline := parseYAMLTag(tag, f.Name)
		if !inline && name == key {
			return f.Type, true
		}
	}

	// Inline struct fields are searched recursively.
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tagIgnored(tag) {
			continue
		}
		_, inline := parseYAMLTag(tag, f.Name)
		if !inline {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Struct {
			if ft2, ok := lookupStructField(ft, key); ok {
				return ft2, true
			}
		}
	}

	// Inline maps receive all otherwise unknown keys.
	for i := 0; i < st.NumField(); i++ {
		f := st.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tagIgnored(tag) {
			continue
		}
		_, inline := parseYAMLTag(tag, f.Name)
		if !inline {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Map {
			return ft.Elem(), true
		}
	}

	return nil, false
}

func parseYAMLTag(tag string, fieldName string) (name string, inline bool) {
	name = strings.ToLower(fieldName)
	if tag == "" {
		return name, false
	}
	parts := strings.Split(tag, ",")
	if parts[0] != "" {
		name = parts[0]
	}
	for _, flag := range parts[1:] {
		if flag == "inline" {
			inline = true
		}
	}
	return name, inline
}

func tagIgnored(tag string) bool {
	if tag == "-" {
		return true
	}
	return tag != "" && strings.SplitN(tag, ",", 2)[0] == "-"
}

func isEmptyInterface(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Interface && t.NumMethod() == 0
}

func typeStops(t reflect.Type) bool {
	if t == nil {
		return true
	}
	if t == reflect.TypeOf(yaml.Node{}) {
		return true
	}
	return typeImplementsUnmarshaler(t)
}

func typeImplementsUnmarshaler(t reflect.Type) bool {
	if t == nil {
		return false
	}
	unmarshalerTypes := [...]reflect.Type{
		reflect.TypeOf((*yaml.Unmarshaler)(nil)).Elem(),
		reflect.TypeOf((*obsoleteUnmarshaler)(nil)).Elem(),
	}
	for _, iface := range unmarshalerTypes {
		if t.Implements(iface) || (t.Kind() != reflect.Ptr && reflect.PointerTo(t).Implements(iface)) {
			return true
		}
	}
	return false
}

type obsoleteUnmarshaler interface {
	UnmarshalYAML(unmarshal func(interface{}) error) error
}
