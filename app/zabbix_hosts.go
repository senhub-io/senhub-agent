package app

import (
	"fmt"
	"sort"
	"strings"
)

type existingHost struct {
	ID        string
	Name      string
	Platform  string
	Templates map[string]string
}

func (s *zabbixSetup) resolveHosts(names []string) error {
	if s.blind() {
		s.say("would link the templates on the host(s) %s", strings.Join(names, ", "))
		return nil
	}

	type hostGetParams struct {
		Output                []string            `json:"output"`
		Filter                map[string][]string `json:"filter"`
		SelectParentTemplates []string            `json:"selectParentTemplates"`
	}
	type hostGetResponse struct {
		HostID          string `json:"hostid"`
		Host            string `json:"host"`
		ParentTemplates []struct {
			TemplateID string `json:"templateid"`
			Host       string `json:"host"`
		} `json:"parentTemplates"`
	}

	params := hostGetParams{
		Output:                []string{"hostid", "host"},
		Filter:                map[string][]string{"host": names},
		SelectParentTemplates: []string{"templateid", "host"},
	}
	var hosts []hostGetResponse
	if err := s.api.call("host.get", params, &hosts); err != nil {
		return fmt.Errorf("reading the hosts to link: %w", err)
	}

	hostMap := make(map[string]hostGetResponse)
	for _, h := range hosts {
		hostMap[h.Host] = h
	}

	var result []existingHost
	for _, name := range names {
		h, ok := hostMap[name]
		if !ok {
			return fmt.Errorf("no host named %q on the server (use the technical name shown on the host page)", name)
		}
		templates := make(map[string]string)
		for _, t := range h.ParentTemplates {
			templates[t.TemplateID] = t.Host
		}
		plat := ""
		found := false
		for _, p := range supportedPlatforms {
			suffix := " " + p
			platformMatched := false
			for _, tname := range templates {
				if strings.HasSuffix(tname, suffix) {
					platformMatched = true
					break
				}
			}
			if platformMatched {
				if found {
					return fmt.Errorf("cannot tell the platform of host %q: it links both linux and windows templates", name)
				}
				found = true
				plat = p
			}
		}
		if !found {
			return fmt.Errorf("cannot tell the platform of host %q: none of the templates it links ends in %q or %q", name, " linux", " windows")
		}
		result = append(result, existingHost{
			ID:        h.HostID,
			Name:      h.Host,
			Platform:  plat,
			Templates: templates,
		})
	}
	s.hosts = result
	return nil
}

func (s *zabbixSetup) linkExistingHosts(platform string, templateNames []string) error {
	if s.blind() || len(s.hosts) == 0 {
		return nil
	}

	// Compared by name: a dry run imports nothing, so the templates it
	// would link have no id yet.
	wanted := append([]string(nil), templateNames...)
	sort.Strings(wanted)

	for _, h := range s.hosts {
		if h.Platform != platform {
			continue
		}
		linkedNames := make(map[string]bool, len(h.Templates))
		for _, n := range h.Templates {
			linkedNames[n] = true
		}
		var missing []string
		for _, n := range wanted {
			if !linkedNames[n] {
				missing = append(missing, n)
			}
		}
		if len(missing) == 0 {
			s.say("host %q already links all %d template(s)", h.Name, len(wanted))
			continue
		}
		if s.dryRun {
			s.say("would link %d template(s) on host %q: %s", len(missing), h.Name, strings.Join(missing, ", "))
			continue
		}
		missingIDs := make([]string, 0, len(missing))
		for _, n := range missing {
			id, ok := s.templateByName[n]
			if !ok {
				return fmt.Errorf("the template %q was imported but cannot be read back", n)
			}
			missingIDs = append(missingIDs, id)
		}
		allIDs := make([]string, 0, len(h.Templates)+len(missingIDs))
		for id := range h.Templates {
			allIDs = append(allIDs, id)
		}
		sort.Strings(allIDs)
		allIDs = append(allIDs, missingIDs...)
		type templateUpdate struct {
			TemplateID string `json:"templateid"`
		}
		type hostUpdateParams struct {
			HostID    string           `json:"hostid"`
			Templates []templateUpdate `json:"templates"`
		}
		var updateParams hostUpdateParams
		updateParams.HostID = h.ID
		for _, id := range allIDs {
			updateParams.Templates = append(updateParams.Templates, templateUpdate{id})
		}
		if err := s.api.call("host.update", updateParams, nil); err != nil {
			return fmt.Errorf("linking the templates on host %q: %w", h.Name, err)
		}
		s.say("linked %d template(s) on host %q: %s", len(missing), h.Name, strings.Join(missing, ", "))
	}
	return nil
}
