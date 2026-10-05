package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/strategies/zabbix/template"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/cliexit"
)

// zabbixAPI is the JSON-RPC client the setup command talks to. It is
// deliberately small: the six methods below are everything preparing a
// server needs, and a thin client is easier to reason about than a
// dependency that must be kept in step with the product's releases.
type zabbixAPI struct {
	url   string
	token string
	http  *http.Client
	// bodyAuth carries the token inside the JSON-RPC envelope. Servers
	// before 6.4 know no other way; 8.0 refuses it and wants the header.
	bodyAuth bool
}

func newZabbixAPI(rawURL, token string) *zabbixAPI {
	u := strings.TrimRight(rawURL, "/")
	if !strings.HasSuffix(u, "/api_jsonrpc.php") {
		u += "/api_jsonrpc.php"
	}
	return &zabbixAPI{url: u, token: token, http: &http.Client{Timeout: 60 * time.Second}}
}

type zabbixError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (e *zabbixError) Error() string {
	if e.Data != "" {
		return e.Message + " " + e.Data
	}
	return e.Message
}

func (a *zabbixAPI) call(method string, params interface{}, out interface{}) error {
	// apiinfo.version is the one method Zabbix refuses when authenticated.
	authenticated := a.token != "" && method != "apiinfo.version"
	envelope := map[string]interface{}{
		"jsonrpc": "2.0", "method": method, "params": params, "id": 1,
	}
	if authenticated && a.bodyAuth {
		envelope["auth"] = a.token
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authenticated && !a.bodyAuth {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: the server answered HTTP %d (is the URL the Zabbix frontend?)", method, resp.StatusCode)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *zabbixError    `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s: the answer is not JSON-RPC (is the URL the Zabbix frontend?)", method)
	}
	if env.Error != nil {
		return fmt.Errorf("%s: %w", method, env.Error)
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// zabbixSetup holds what one run decides, so the steps read as a recipe
// rather than as a chain of arguments.
type zabbixSetup struct {
	api           *zabbixAPI
	group         string
	metadata      string
	discoveryWait string
	dryRun        bool
	templates     map[string][]byte // probe type -> exported YAML
	// hostGroupsOnly is a server before 6.2, which has no template
	// groups: its import rule for them is named groups.
	hostGroupsOnly bool
	linked         []string
	out            io.Writer // nil means standard output
}

// blind reports that the run can describe but not look: a dry run given
// no token. Everything it says is then what it would do, never what it
// found, and it says so rather than failing on the first read.
func (s *zabbixSetup) blind() bool { return s.dryRun && s.api.token == "" }

func (s *zabbixSetup) say(format string, args ...interface{}) {
	prefix := "  "
	if s.dryRun {
		prefix = "  [dry run] "
	}
	out := s.out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintf(out, prefix+format+"\n", args...)
}

// version checks the server answers and is recent enough for the export
// format the templates use.
func (s *zabbixSetup) version() (string, error) {
	var v string
	if err := s.api.call("apiinfo.version", map[string]interface{}{}, &v); err != nil {
		return "", err
	}
	return v, nil
}

func (s *zabbixSetup) importTemplates() error {
	rules := map[string]interface{}{
		"templates":      map[string]bool{"createMissing": true, "updateExisting": true},
		"items":          map[string]bool{"createMissing": true, "updateExisting": true},
		"discoveryRules": map[string]bool{"createMissing": true, "updateExisting": true},
		"valueMaps":      map[string]bool{"createMissing": true, "updateExisting": true},
	}
	if s.hostGroupsOnly {
		rules["groups"] = map[string]bool{"createMissing": true}
	} else {
		rules["template_groups"] = map[string]bool{"createMissing": true}
	}
	for _, probe := range sortedKeys(s.templates) {
		if s.dryRun {
			s.say("would import the template of %s (%d bytes)", probe, len(s.templates[probe]))
			continue
		}
		err := s.api.call("configuration.import", map[string]interface{}{
			"format": "yaml", "source": string(s.templates[probe]), "rules": rules,
		}, nil)
		if err != nil {
			return fmt.Errorf("importing the template of %s: %w", probe, err)
		}
		s.say("template of %s imported", probe)
	}
	return nil
}

// ensureGroup returns the host group id, creating the group when it is
// absent. Re-running the command finds it and changes nothing.
func (s *zabbixSetup) ensureGroup() (string, error) {
	if s.blind() {
		s.say("would create the host group %q if it does not exist", s.group)
		return "0", nil
	}
	var found []struct {
		GroupID string `json:"groupid"`
	}
	if err := s.api.call("hostgroup.get", map[string]interface{}{
		"filter": map[string]interface{}{"name": []string{s.group}}, "output": []string{"groupid"},
	}, &found); err != nil {
		return "", err
	}
	if len(found) > 0 {
		s.say("host group %q already exists", s.group)
		return found[0].GroupID, nil
	}
	if s.dryRun {
		s.say("would create the host group %q", s.group)
		return "0", nil
	}
	var created struct {
		GroupIDs []string `json:"groupids"`
	}
	if err := s.api.call("hostgroup.create", map[string]interface{}{"name": s.group}, &created); err != nil {
		return "", err
	}
	s.say("host group %q created", s.group)
	return created.GroupIDs[0], nil
}

// templateIDs reads back the templates that were just imported, by the
// names the generator gave them.
func (s *zabbixSetup) templateIDs(names []string) ([]string, error) {
	if s.blind() {
		return nil, nil
	}
	var found []struct {
		TemplateID string `json:"templateid"`
		Host       string `json:"host"`
	}
	if err := s.api.call("template.get", map[string]interface{}{
		"filter": map[string]interface{}{"host": names}, "output": []string{"templateid", "host"},
	}, &found); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(found))
	for _, t := range found {
		ids = append(ids, t.TemplateID)
		s.linked = append(s.linked, t.Host)
	}
	return ids, nil
}

// ensureAction creates, or replaces, the autoregistration action that
// turns a first contact into a host carrying the templates. Replacing
// rather than patching keeps a re-run idempotent whatever the previous
// state was.
func (s *zabbixSetup) ensureAction(name, metadata, groupID string, templateIDs []string) error {
	if s.blind() {
		s.say("would create or replace the action %q, matching host metadata containing %q, linking %d template(s)",
			name, metadata, len(s.templates))
		return nil
	}
	var found []struct {
		ActionID string `json:"actionid"`
	}
	if err := s.api.call("action.get", map[string]interface{}{
		"filter": map[string]interface{}{"name": []string{name}}, "output": []string{"actionid"},
	}, &found); err != nil {
		return err
	}
	optemplate := make([]map[string]string, 0, len(templateIDs))
	for _, id := range templateIDs {
		optemplate = append(optemplate, map[string]string{"templateid": id})
	}
	params := map[string]interface{}{
		"name":        name,
		"eventsource": 2, // autoregistration
		"status":      0,
		"filter": map[string]interface{}{"evaltype": 0, "conditions": []map[string]interface{}{
			{"conditiontype": 24, "operator": 2, "value": metadata}, // host metadata contains
		}},
		"operations": []map[string]interface{}{
			{"operationtype": 2}, // add host
			{"operationtype": 4, "opgroup": []map[string]string{{"groupid": groupID}}}, // add to group
			{"operationtype": 6, "optemplate": optemplate},                             // link templates
			// Automatic inventory, without which the items that carry the
			// machine's operating system, hardware and serial number
			// arrive and fill nothing: a field is only populated on a host
			// whose inventory mode says so.
			{"operationtype": 10, "opinventory": map[string]int{"inventory_mode": 1}},
		},
	}
	if s.dryRun {
		// The templates are not imported on a dry run, so they have no id
		// to count yet: what would be linked is what would be imported.
		s.say("would %s the action %q, matching host metadata containing %q, linking %d template(s)",
			map[bool]string{true: "replace", false: "create"}[len(found) > 0], name, metadata, len(s.templates))
		return nil
	}
	if len(found) > 0 {
		params["actionid"] = found[0].ActionID
		delete(params, "eventsource")
		if err := s.api.call("action.update", params, nil); err != nil {
			return err
		}
		s.say("action %q updated, linking %d template(s)", name, len(templateIDs))
		return nil
	}
	if err := s.api.call("action.create", params, nil); err != nil {
		return err
	}
	s.say("action %q created, linking %d template(s)", name, len(templateIDs))
	return nil
}

// quickenDiscovery lowers the update interval of the discovery rules the
// templates carry. Zabbix defaults them to an hour, which is right for a
// fleet and wrong for the first minutes of a deployment, where an empty
// host reads as a failure.
func (s *zabbixSetup) quickenDiscovery(templateIDs []string) error {
	if s.discoveryWait == "" {
		return nil
	}
	if s.blind() {
		s.say("would set the discovery rules to run every %s", s.discoveryWait)
		return nil
	}
	if len(templateIDs) == 0 {
		return nil
	}
	var rules []struct {
		ItemID string `json:"itemid"`
		Key    string `json:"key_"`
	}
	if err := s.api.call("discoveryrule.get", map[string]interface{}{
		"templateids": templateIDs, "output": []string{"itemid", "key_"},
	}, &rules); err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	if s.dryRun {
		s.say("would set %d discovery rule(s) to run every %s", len(rules), s.discoveryWait)
		return nil
	}
	for _, r := range rules {
		if err := s.api.call("discoveryrule.update", map[string]interface{}{
			"itemid": r.ItemID, "delay": s.discoveryWait,
		}, nil); err != nil {
			return fmt.Errorf("setting the interval of %s: %w", r.Key, err)
		}
	}
	s.say("%d discovery rule(s) now run every %s", len(rules), s.discoveryWait)
	return nil
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// tokenFrom reads the API token from the flag, a file, or the
// environment, in that order. A token on the command line is visible to
// every process on the machine, so the other two exist.
func tokenFrom(flagValue, filePath string) (string, error) {
	if filePath != "" {
		raw, err := os.ReadFile(filePath) // #nosec G304 - the operator names this file
		if err != nil {
			return "", fmt.Errorf("reading the token file: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if flagValue != "" {
		return flagValue, nil
	}
	if v := strings.TrimSpace(os.Getenv("SENHUB_ZABBIX_TOKEN")); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("an API token is required: pass --token-file, --token, or set SENHUB_ZABBIX_TOKEN")
}

// runZabbixSetup prepares a Zabbix server to receive SenHub agents. It
// is the server half of a deployment, run once by an administrator; the
// agents themselves register with no credentials at all.
func runZabbixSetup(args []string) {
	var (
		rawURL, token, tokenFile string
		group                    = "SenHub Agents"
		metadata                 = "senhub-agent"
		actionName               = "Autoregistration — SenHub Agent"
		discoveryDelay           = "5m"
		dryRun                   bool
		probes                   []string
		allProbes                bool
		opts                     = template.Options{}
	)
	for i := 0; i < len(args); i++ {
		flag := args[i]
		value := func() string {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s needs a value\n", flag)
				os.Exit(cliexit.Failure)
			}
			i++
			return args[i]
		}
		switch flag {
		case "--url":
			rawURL = value()
		case "--token":
			token = value()
		case "--token-file":
			tokenFile = value()
		case "--group":
			group = value()
		case "--metadata":
			metadata = value()
		case "--action-name":
			actionName = value()
		case "--discovery-delay":
			discoveryDelay = value()
		case "--no-discovery-delay":
			discoveryDelay = ""
		case "--probe":
			probes = append(probes, value())
		case "--all-probes":
			allProbes = true
		case "--prefix":
			opts.Prefix = value()
		case "--version":
			opts.Version = value()
		case "--dry-run":
			dryRun = true
		case "--help", "-h":
			fmt.Println(zabbixUsage)
			return
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown option %s\n%s\n", flag, zabbixUsage)
			os.Exit(cliexit.Failure)
		}
	}
	if rawURL == "" {
		fmt.Fprintln(os.Stderr, "Error: --url is required (the Zabbix frontend, for example https://zabbix.example.com)")
		os.Exit(cliexit.Failure)
	}
	if opts.Version != "" && opts.Version != "6.0" && opts.Version != "7.0" {
		fmt.Fprintln(os.Stderr, "Error: --version must be 6.0 or 7.0")
		os.Exit(cliexit.Failure)
	}
	if resolved, err := tokenFrom(token, tokenFile); err == nil {
		token = resolved
	} else if !dryRun {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}

	// Without --probe the command used to link every template it could
	// generate, so a Linux host autoregistered carrying Hyper-V, Veeam,
	// NetScaler and the rest: fifty discovery rules the agent will never
	// answer, which is the same empty line an operator reads as a defect.
	// The default is what every machine runs.
	//
	// A named probe is added to that default rather than replacing it.
	// Replacing looked tidier and was a trap: an operator adding one
	// commercial template with --probe silently unlinked the processor,
	// the memory, the network and the disks from the action, and every
	// host registering afterwards came up with none of them.
	linkedByDefault := len(probes) == 0 && !allProbes

	s := &zabbixSetup{
		api: newZabbixAPI(rawURL, token), group: group, metadata: metadata,
		discoveryWait: discoveryDelay, dryRun: dryRun,
	}

	fmt.Printf("Preparing %s\n", rawURL)
	version, err := s.version()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}
	s.say("Zabbix %s answered", version)
	if zabbixBefore(version, 6, 4) {
		s.api.bodyAuth = true
	}
	if zabbixBefore(version, 6, 2) {
		s.hostGroupsOnly = true
		switch opts.Version {
		case "":
			opts.Version = "6.0"
			s.say("exporting the templates in the 6.0 format this server reads")
		case "7.0":
			fmt.Fprintf(os.Stderr, "Error: Zabbix %s cannot import the 7.0 export format; drop --version or pass --version 6.0\n", version)
			os.Exit(cliexit.Failure)
		}
	}

	groupID, err := s.ensureGroup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}

	// One set of templates per platform, and one autoregistration action
	// per platform to link it. A Windows performance counter declared on
	// a Linux host can never receive a value, and an operator reads that
	// empty line as a defect rather than as an absence. The agent says
	// which platform it is on in its host metadata, so the server picks
	// without anyone choosing.
	for _, platform := range supportedPlatforms {
		platOpts := opts
		platOpts.Platform = platform
		defs, derr := transformers.Definitions()
		if derr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", derr)
			os.Exit(cliexit.Failure)
		}
		linked := withDefaultProbes(defs, platform, probes)
		if allProbes {
			linked = linked[:0]
			for name := range defs {
				linked = append(linked, name)
			}
			sort.Strings(linked)
		}
		rendered, names, rerr := renderTemplates(linked, platOpts)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", rerr)
			os.Exit(cliexit.Failure)
		}
		s.templates = rendered
		fmt.Printf("  for %s:\n", platform)
		if err := s.importTemplates(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(cliexit.Failure)
		}
		var ids []string
		if !dryRun {
			if ids, err = s.templateIDs(names); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(cliexit.Failure)
			}
			if len(ids) == 0 {
				fmt.Fprintln(os.Stderr, "Error: the templates were imported but cannot be read back; check the account's permissions")
				os.Exit(cliexit.Failure)
			}
		}
		if err := s.ensureAction(actionName+" ("+platform+")", metadata+" "+platform, groupID, ids); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(cliexit.Failure)
		}
		if err := s.quickenDiscovery(ids); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(cliexit.Failure)
		}
	}

	if err := s.reportCollidingActions(actionName, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}
	if err := s.retireUnsplitAction(actionName); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cliexit.Failure)
	}

	fmt.Println()
	if linkedByDefault {
		fmt.Println("Linked the probes every machine runs (the host's processor, memory,")
		fmt.Println("network, disks, processes, and on Windows the services and the event")
		fmt.Println("log counter). Name others with --probe (hyperv, mssql, ...) to have")
		fmt.Println("them linked as well, or pass --all-probes; a template linked to a")
		fmt.Println("host whose agent does not run that probe only adds discovery rules")
		fmt.Println("that stay empty.")
	} else if allProbes {
		fmt.Println("Linked every probe template of each platform.")
	} else {
		fmt.Printf("Linked the probes every machine runs, plus what you named: %s.\n", strings.Join(probes, ", "))
	}

	fmt.Println()
	server := hostOf(rawURL)
	fmt.Println("The server is ready. On every machine to monitor, install the agent")
	fmt.Println("with the server's address:")
	fmt.Println()
	fmt.Print(serverAddressNote(server))
	fmt.Println()
	fmt.Printf("  Windows:    msiexec /i senhub-agent-<version>-amd64.msi /qn ZABBIX_SERVER=%s\n", server)
	fmt.Printf("  Linux:      sudo /usr/local/bin/senhub-agent config init --zabbix-server %s\n", server)
	fmt.Printf("  Container:  SENHUB_ZABBIX_SERVER=%s\n", server)
	fmt.Println()
	fmt.Println("or write the two lines yourself in strategies.d/20-zabbix.yaml:")
	fmt.Println()
	fmt.Println("  zabbix:")
	fmt.Printf("    server: %q\n", server)
	fmt.Println()
	fmt.Println("It registers by itself at its first contact. Nothing else to do,")
	fmt.Println("and nothing to type in the Zabbix interface.")
}

// hostOf keeps the host of the frontend URL and no port: the setup only
// talks to the frontend, and the trapper port an agent pushes to is set
// in the server's own configuration, which the API does not expose. The
// agent adds 10051 to an address without a port.
func hostOf(rawURL string) string {
	u := rawURL
	for _, p := range []string{"https://", "http://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.IndexAny(u, "/:"); i >= 0 {
		u = u[:i]
	}
	return u
}

func serverAddressNote(host string) string {
	return fmt.Sprintf("  %s is the frontend's host. Without a port the agent uses 10051;\n"+
		"  if the server or proxy listens elsewhere (ListenPort in its\n"+
		"  configuration), or runs on another machine, write host:port instead.\n", host)
}

// supportedPlatforms are the operating systems the agent is published
// for, and therefore the template sets a server needs to carry.
var supportedPlatforms = []string{"linux", "windows"}

// retireUnsplitAction disables the single action earlier versions
// created. Zabbix runs every matching autoregistration action, so
// leaving it enabled would link the platform-blind templates beside the
// platform ones and bring back the empty items this split removes.
func (s *zabbixSetup) retireUnsplitAction(name string) error {
	if s.blind() {
		s.say("would disable the earlier single action %q if it exists", name)
		return nil
	}
	var found []struct {
		ActionID string `json:"actionid"`
		Status   string `json:"status"`
	}
	if err := s.api.call("action.get", map[string]interface{}{
		"filter": map[string]interface{}{"name": []string{name}}, "output": []string{"actionid", "status"},
	}, &found); err != nil {
		return err
	}
	if len(found) == 0 || found[0].Status == "1" {
		return nil
	}
	if s.dryRun {
		s.say("would disable the earlier single action %q", name)
		return nil
	}
	if err := s.api.call("action.update", map[string]interface{}{
		"actionid": found[0].ActionID, "status": 1,
	}, nil); err != nil {
		return err
	}
	s.say("earlier single action %q disabled; the per-platform ones replace it", name)
	return nil
}

// reportCollidingActions names any other enabled autoregistration
// action a registering agent would also match.
//
// Zabbix runs every action whose condition matches, and two that both
// link templates do not merge: the second link fails, because Zabbix
// refuses two linked templates declaring one key, and it fails in
// silence. A host then comes up carrying whichever set won, with items
// that never fill and nothing anywhere saying why. That happened on a
// server prepared by hand months earlier.
//
// It is reported and not disabled. An action an operator wrote may
// carry operations this command knows nothing about, and switching it
// off unasked is not a preparation command's business. Naming it, and
// saying what it will do, leaves the decision where it belongs.
func (s *zabbixSetup) reportCollidingActions(ownName, metadata string) error {
	if s.blind() {
		s.say("would look for other autoregistration actions a registering agent also matches")
		return nil
	}
	var found []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Filter struct {
			Conditions []struct {
				ConditionType string `json:"conditiontype"`
				Operator      string `json:"operator"`
				Value         string `json:"value"`
			} `json:"conditions"`
		} `json:"filter"`
	}
	if err := s.api.call("action.get", map[string]interface{}{
		"output": []string{"name", "status"}, "selectFilter": "extend",
		"filter": map[string]interface{}{"eventsource": 2},
	}, &found); err != nil {
		return err
	}
	for _, a := range found {
		if a.Status != "0" || strings.HasPrefix(a.Name, ownName) {
			continue
		}
		for _, c := range a.Filter.Conditions {
			// 24 is the host metadata; operator 2 is "contains", the
			// only one this can reason about without guessing.
			if c.ConditionType != "24" || c.Operator != "2" || c.Value == "" {
				continue
			}
			for _, platform := range supportedPlatforms {
				if !strings.Contains(metadata+" "+platform, c.Value) {
					continue
				}
				fmt.Printf("\n  WARNING: the action %q also matches an agent registering as %q.\n",
					a.Name, metadata+" "+platform)
				fmt.Println("  Zabbix runs both, and because the two template sets declare the")
				fmt.Println("  same keys the newer link fails silently: the host comes up with")
				fmt.Println("  the other set and items that never fill. Disable it, or narrow")
				fmt.Println("  its condition, before the next agent registers.")
				break
			}
		}
	}
	return nil
}

// withDefaultProbes returns the probes to link on one platform: the
// universal ones whose template carries something there, plus whatever
// was named, each once and in a stable order. The universal flag lives
// in the definitions, so a new host probe is linked by declaring it
// rather than by editing this command.
func withDefaultProbes(defs map[string]transformers.ProbeDefinition, platform string, named []string) []string {
	var out []string
	for name, def := range defs {
		if !def.Universal {
			continue
		}
		hasMetric := false
		for _, m := range def.Metrics {
			if m.RunsOn(platform) && (m.Otel == nil || !m.Otel.Skip) {
				hasMetric = true
				break
			}
		}
		if hasMetric {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	seen := map[string]bool{}
	for _, p := range out {
		seen[p] = true
	}
	for _, p := range named {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// zabbixBefore reports whether a version string from apiinfo.version is
// older than major.minor. An answer it cannot read counts as recent, the
// line every current default is written for.
func zabbixBefore(version string, major, minor int) bool {
	var ma, mi int
	if _, err := fmt.Sscanf(version, "%d.%d", &ma, &mi); err != nil {
		return false
	}
	return ma < major || (ma == major && mi < minor)
}
