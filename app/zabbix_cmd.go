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
                                    [--out <directory>]

Writes the Zabbix templates generated from the probe definitions, one
file per probe type, named senhub-<type>-<version>.yaml. Without --probe,
every definition is written. With one --probe and no --out, the template
goes to standard output.`

func runZabbixCommand() {
	args := os.Args[2:]
	if len(args) == 0 || args[0] != "template" {
		fmt.Fprintln(os.Stderr, zabbixUsage)
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
		case "--help", "-h":
			fmt.Println(zabbixUsage)
			return
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n%s\n", flag, zabbixUsage)
			os.Exit(2)
		}
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
		body, err := template.Encode(exp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", p, err)
			os.Exit(1)
		}
		if out == "" {
			os.Stdout.Write(body)
			continue
		}
		path := filepath.Join(out, fmt.Sprintf("senhub-%s-%s.yaml", p, exp.ZabbixExport.Version))
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
