package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rs/zerolog"

	"senhub-agent.go/internal/agent/services/data_store/strategies/http"
	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
)

func init() {
	RegisterCommand(ExtraCommand{Name: "zabbix", ReadOnly: true, Run: runZabbixCommand})
}

const zabbixUsage = `Usage: senhub-agent zabbix template [--probe <type> ...] [--version 6.0|7.0]
                                    [--prefix <key prefix>] [--delay <interval>]
                                    [--out <directory>] [--platform linux|windows]

       senhub-agent zabbix setup --url <frontend> [--token-file <path>]
                                 [--group <name>] [--metadata <string>]
                                 [--action-name <name>] [--probe <type> ...]
                                 [--discovery-delay <interval> | --no-discovery-delay]
                                 [--prefix <key prefix>] [--version 6.0|7.0] [--dry-run]

template writes the Zabbix templates generated from the probe
definitions, one file per probe type, named senhub-<type>-<version>.yaml.
Without --probe, every definition is written. With one --probe and no
--out, the template goes to standard output.

setup does the whole server side in one call: it imports those same
templates, creates the host group, and creates the autoregistration
action that turns an agent's first contact into a host carrying them.
Without --probe it links the probes every machine runs; naming others
adds them, and a template whose probe an agent does not run only
contributes discovery rules that never answer.
After it, a machine needs nothing but the agent and two lines naming the
server. Run it once, as an administrator; a deployed agent never holds
an API token. Re-running it is safe: every step is idempotent, which is
also how a template is refreshed after an upgrade.

The token is read from --token-file, then --token, then the environment
variable SENHUB_ZABBIX_TOKEN. Prefer a file: a token on the command line
is visible to every process on the machine.`

func runZabbixCommand() {
	args := os.Args[2:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, zabbixUsage)
		os.Exit(2)
	}
	switch args[0] {
	case "template":
	case "setup":
		runZabbixSetup(args[1:])
		return
	case "--help", "-h", "help":
		fmt.Println(zabbixUsage)
		return
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown subcommand %q\n%s\n", args[0], zabbixUsage)
		os.Exit(2)
	}
	opts := template.Options{}
	var probes []string
	out := ""
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		flag := rest[i]
		value := func() string {
			if i+1 >= len(rest) {
				fmt.Fprintf(os.Stderr, "Error: %s needs a value\n", flag)
				os.Exit(2)
			}
			i++
			return rest[i]
		}
		switch flag {
		case "--probe":
			probes = append(probes, value())
		case "--version":
			opts.Version = value()
		case "--prefix":
			opts.Prefix = value()
		case "--delay":
			opts.ItemDelay = value()
		case "--out":
			out = value()
		case "--platform":
			opts.Platform = value()
		case "--help", "-h":
			fmt.Println(zabbixUsage)
			return
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n%s\n", flag, zabbixUsage)
			os.Exit(2)
		}
	}
	if opts.Platform != "" && opts.Platform != "linux" && opts.Platform != "windows" {
		fmt.Fprintln(os.Stderr, "Error: --platform must be linux or windows")
		os.Exit(2)
	}
	if opts.Version != "" && opts.Version != "6.0" && opts.Version != "7.0" {
		fmt.Fprintln(os.Stderr, "Error: --version must be 6.0 or 7.0")
		os.Exit(2)
	}

	defs, err := transformers.Definitions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(probes) == 0 {
		for name := range defs {
			probes = append(probes, name)
		}
		sort.Strings(probes)
	}
	nop := zerolog.Nop()
	if lookups, lerr := http.NewLookupRegistry(&nop); lerr == nil {
		opts.Lookups = lookupAdapter{lookups}
	}

	if out == "" && len(probes) > 1 {
		out = "."
	}
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	if out != "" {
		body, err := template.Encode(template.Base(opts))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		path := filepath.Join(out, fmt.Sprintf("senhub-agent-%s.yaml", firstNonEmptyVersion(opts.Version)))
		if err := os.WriteFile(path, body, 0o644); err != nil { // #nosec G306 - a template to import, not a secret
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(path)
	}

	for _, p := range probes {
		def, ok := defs[p]
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: no definition for probe type %q\n", p)
			os.Exit(1)
		}
		exp, err := template.Generate(def, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", p, err)
			os.Exit(1)
		}
		if exp.DeclaresNothing() {
			fmt.Fprintf(os.Stderr, "Note: %s relays records rather than metrics; it declares no Zabbix item, so no template was written for it.\n", p)
			continue
		}
		body, err := template.Encode(exp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", p, err)
			os.Exit(1)
		}
		if out == "" {
			os.Stdout.Write(body)
			continue
		}
		path := filepath.Join(out, fmt.Sprintf("senhub-%s%s-%s.yaml", p, platformSuffix(opts.Platform), exp.ZabbixExport.Version))
		if err := os.WriteFile(path, body, 0o644); err != nil { // #nosec G306 - a template to import, not a secret
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(path)
	}
}

// lookupAdapter exposes the HTTP output's lookup registry as the code →
// text source the template generator turns into value maps.
type lookupAdapter struct{ reg *http.LookupRegistry }

func (a lookupAdapter) Lookup(id string) (map[int]string, bool) {
	def, ok := a.reg.GetLookup(strings.TrimSpace(id))
	if !ok {
		return nil, false
	}
	out := make(map[int]string, len(def.Mappings))
	for code, v := range def.Mappings {
		out[code] = v.Text
	}
	return out, true
}

// Severities gives each code the severity the lookup classes it under,
// which the generator turns into triggers.
func (a lookupAdapter) Severities(id string) (map[int]string, bool) {
	def, ok := a.reg.GetLookup(strings.TrimSpace(id))
	if !ok {
		return nil, false
	}
	out := make(map[int]string, len(def.Mappings))
	for code, v := range def.Mappings {
		out[code] = v.Severity
	}
	return out, true
}

// renderTemplates generates the templates of the named probe types, or
// of every definition when none is named, and returns them keyed by
// probe type along with the template names Zabbix will know them by.
// Both subcommands go through it, so what setup imports is byte for byte
// what template writes.
func renderTemplates(probes []string, opts template.Options) (map[string][]byte, []string, error) {
	defs, err := transformers.Definitions()
	if err != nil {
		return nil, nil, err
	}
	if len(probes) == 0 {
		for name := range defs {
			probes = append(probes, name)
		}
		sort.Strings(probes)
	}
	nop := zerolog.Nop()
	if lookups, lerr := http.NewLookupRegistry(&nop); lerr == nil {
		opts.Lookups = lookupAdapter{lookups}
	}
	out := make(map[string][]byte, len(probes)+1)
	var names []string

	// The agent's own items travel in a template of their own: Zabbix
	// refuses two linked templates declaring one key, so they cannot be
	// repeated in each probe template.
	base := template.Base(opts)
	baseBody, err := template.Encode(base)
	if err != nil {
		return nil, nil, fmt.Errorf("base template: %w", err)
	}
	out[baseTemplateKey] = baseBody
	names = append(names, template.BaseName)

	for _, p := range probes {
		def, ok := defs[p]
		if !ok {
			return nil, nil, fmt.Errorf("no definition for probe type %q", p)
		}
		exp, err := template.Generate(def, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", p, err)
		}
		body, err := template.Encode(exp)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", p, err)
		}
		out[p] = body
		for _, t := range exp.ZabbixExport.Templates {
			names = append(names, t.Template)
		}
	}
	return out, names, nil
}

// baseTemplateKey names the base template in the map renderTemplates
// returns. A probe type is a bare lowercase identifier, so a phrase with
// spaces cannot collide with one, and it reads properly in the output.
const baseTemplateKey = "the agent itself"

func firstNonEmptyVersion(v string) string {
	if v == "" {
		return "7.0"
	}
	return v
}

func platformSuffix(platform string) string {
	if platform == "" {
		return ""
	}
	return "-" + platform
}
