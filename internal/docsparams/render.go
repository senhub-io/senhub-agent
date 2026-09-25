// Package docsparams renders a probe's declared parameter schema as the
// markdown table its documentation page carries.
//
// The tables used to be written by hand, and drifted: pages documented
// keys the parser had stopped reading, and omitted keys it read. The
// schema is derived from the parser and guarded by a test, so it is the
// one description that cannot lie; rendering the page from it makes the
// drift impossible rather than merely detectable (#859).
package docsparams

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"senhub-agent.go/internal/agent/probes/spec"
)

// Markers delimit the generated block inside a page. Everything outside
// them is written by hand and is never touched.
const (
	Start = "<!-- schema:params:start -->"
	End   = "<!-- schema:params:end -->"
)

// Render returns the block a page carries between the markers, markers
// included, terminated by a newline.
func Render(p spec.Probe) string {
	var body strings.Builder
	if len(p.Params) == 0 {
		// The cadence is the one thing a reader still wants when there is
		// nothing to set, and it is fixed in the code for these probes.
		if p.DefaultInterval > 0 {
			fmt.Fprintf(&body, "This probe reads no parameters. It collects every %d seconds, a cadence fixed in the code.\n\n", p.DefaultInterval)
		} else {
			body.WriteString("This probe reads no parameters.\n\n")
		}
	} else {
		body.WriteString("| Parameter | Must set | Default | Description |\n")
		body.WriteString("|---|---|---|---|\n")
		for _, row := range rows(p.Params, "") {
			body.WriteString(row)
		}
		body.WriteString("\n")
	}
	var b strings.Builder
	b.WriteString(Start + "\n")
	b.WriteString("<!-- Generated from the probe's schema. Run `make docs-params` after changing it. -->\n")
	b.WriteString(sealLine(body.String()) + "\n\n")
	b.WriteString(body.String())
	b.WriteString(End + "\n")
	return b.String()
}

// sealPrefix opens the line that carries the digest of a block's body.
const sealPrefix = "<!-- sha256:"

// sealLine is the digest of a generated body. The commercial probe pages
// are generated from the enterprise repository, whose guard never runs
// here, so a hand edit inside one of their blocks merged green; the seal
// lets this repository tell a generated block from an edited one without
// the schema that produced it.
func sealLine(body string) string {
	sum := sha256.Sum256([]byte(body))
	return sealPrefix + hex.EncodeToString(sum[:]) + " -->"
}

// VerifyBlock reports whether the generated block of a page still matches
// the digest it was written with. has is false when the page carries no
// block. A block without a seal is reported as not matching.
func VerifyBlock(page string) (has bool, ok bool) {
	start := strings.Index(page, Start)
	end := strings.Index(page, End)
	if start < 0 || end < start {
		return false, false
	}
	block := page[start+len(Start) : end]
	i := strings.Index(block, sealPrefix)
	if i < 0 {
		return true, false
	}
	lineEnd := strings.Index(block[i:], "\n")
	if lineEnd < 0 {
		return true, false
	}
	seal := block[i : i+lineEnd]
	body := strings.TrimPrefix(block[i+lineEnd+1:], "\n")
	return true, seal == sealLine(body)
}

// rows renders one level, then the fields of every block below it, so a
// nested key appears under its dotted path exactly as it is written.
func rows(params []spec.ParamSpec, prefix string) []string {
	var out []string
	for _, p := range params {
		out = append(out, line(p, prefix))
		switch p.Kind {
		case spec.KindBlock:
			out = append(out, rows(p.Fields, prefix+p.Key+".")...)
		case spec.KindBlockList:
			// A list of blocks: its fields belong to each entry.
			out = append(out, rows(p.Fields, prefix+p.Key+"[].")...)
		}
	}
	return out
}

// mustSet answers the question the column asks, which is "do I have to
// set this", not "does the parser refuse without it". Those differ: a
// database password is accepted absent by the parser and the probe then
// collects nothing, and a table that answers No there is answering a
// question nobody asked. Three values instead of two, and the condition
// spelled out when there is one.
func mustSet(p spec.ParamSpec) string {
	if p.Required {
		return "Yes"
	}
	if len(p.EssentialWhen) > 0 {
		parts := make([]string, 0, len(p.EssentialWhen))
		for _, c := range p.EssentialWhen {
			parts = append(parts, "`"+c.Key+"` is "+quotedList(c.Values))
		}
		return "If " + strings.Join(parts, " and ")
	}
	if p.Essential {
		return "In practice"
	}
	return "No"
}

func line(p spec.ParamSpec, prefix string) string {
	req := mustSet(p)
	desc := strings.TrimSpace(p.Description)
	if desc == "" {
		desc = kindWord(p.Kind)
	}
	if len(p.Enum) > 0 {
		desc += ". One of " + quotedList(p.Enum)
	}
	if len(p.AlsoAccepts) > 0 {
		alt := append([]string{}, p.AlsoAccepts...)
		sort.Strings(alt)
		desc += ". Also accepted: " + quotedList(alt)
	}
	if p.Secret {
		desc += ". A secret: reference it with `${secret:…}`, `${env:…}` or `${file:…}` rather than writing it in the file"
	}
	if p.Example != "" {
		desc += ". Example: `" + p.Example + "`"
	}
	return fmt.Sprintf("| `%s%s` | %s | %s | %s |\n", prefix, p.Key, req, defaultCell(p), escapePipes(desc))
}

func defaultCell(p spec.ParamSpec) string {
	if p.Default == nil {
		return "-"
	}
	switch v := p.Default.(type) {
	case string:
		if v == "" {
			return "`\"\"`"
		}
		return "`" + v + "`"
	case bool:
		return fmt.Sprintf("`%t`", v)
	default:
		return fmt.Sprintf("`%v`", v)
	}
}

func kindWord(k spec.ParamKind) string {
	switch k {
	case spec.KindBlock:
		return "A block of settings"
	case spec.KindBlockList:
		return "A list of blocks"
	case spec.KindMap:
		return "A map of key and value"
	case spec.KindStringList:
		return "A list of strings"
	default:
		return "A " + string(k)
	}
}

func quotedList(vs []string) string {
	q := make([]string, len(vs))
	for i, v := range vs {
		q[i] = "`" + v + "`"
	}
	return strings.Join(q, ", ")
}

// escapePipes keeps a description with a pipe from breaking the table.
func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
