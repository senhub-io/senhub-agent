package template

import (
	"fmt"
)

// What a Zabbix server enforces on import, pinned here so the generator is
// held to it offline. Each limit was measured by importing into a live
// 7.0 and 8.0 server, not read off a changelog: a value past it is refused
// with a message that names the field and nothing else.
const (
	// maxHostName bounds a template's technical name, validated as a host
	// name: the character set is enforced by technicalName.
	maxHostName = 128
	// maxItemName bounds the name of an item, a prototype and a discovery
	// rule. A rule whose name concatenated fifteen dimensions ran past it.
	maxItemName = 255
	// maxValueMapName bounds a value map's name.
	maxValueMapName = 64
)

// Validate reports every way an export would be refused by the server the
// version targets. An empty result means the file imports; the generator's
// tests hold every embedded definition to that.
func Validate(exp Export) []string {
	var out []string
	body := exp.ZabbixExport
	switch body.Version {
	case "6.0":
		if len(body.Groups) == 0 || len(body.TemplateGroups) > 0 {
			out = append(out, "a 6.0 export declares its template groups under `groups`")
		}
	default:
		if len(body.TemplateGroups) == 0 || len(body.Groups) > 0 {
			out = append(out, fmt.Sprintf("a %s export declares its template groups under `template_groups`", body.Version))
		}
	}
	for _, t := range body.Templates {
		if t.Template != technicalName(t.Template) {
			out = append(out, fmt.Sprintf("template %q: technical name holds a character Zabbix refuses in a host name", t.Template))
		}
		if n := len([]rune(t.Template)); n > maxHostName {
			out = append(out, fmt.Sprintf("template %q: technical name is %d characters, over %d", t.Template, n, maxHostName))
		}
		if n := len([]rune(t.Name)); n > maxHostName {
			out = append(out, fmt.Sprintf("template %q: visible name is %d characters, over %d", t.Template, n, maxHostName))
		}
		for _, it := range t.Items {
			if n := len([]rune(it.Name)); n > maxItemName {
				out = append(out, fmt.Sprintf("template %q: item %q is %d characters, over %d", t.Template, it.Key, n, maxItemName))
			}
		}
		for _, r := range t.DiscoveryRules {
			if n := len([]rune(r.Name)); n > maxItemName {
				out = append(out, fmt.Sprintf("template %q: discovery rule %q has a %d-character name, over %d", t.Template, r.Key, n, maxItemName))
			}
			for _, p := range r.ItemPrototypes {
				if n := len([]rune(p.Name)); n > maxItemName {
					out = append(out, fmt.Sprintf("template %q: prototype %q has a %d-character name, over %d", t.Template, p.Key, n, maxItemName))
				}
			}
		}
		for _, vm := range t.ValueMaps {
			if n := len([]rune(vm.Name)); n > maxValueMapName {
				out = append(out, fmt.Sprintf("template %q: value map %q is %d characters, over %d", t.Template, vm.Name, n, maxValueMapName))
			}
		}
	}
	return out
}
