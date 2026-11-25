package app

import (
	"fmt"
	"io"
	"strings"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/cliexit"
)

// regenerateKeyReport is the JSON view of a successful key regeneration.
// The old key never appears in it: the whole point is to retire that value.
type regenerateKeyReport struct {
	jsonHeader
	AgentKey          string `json:"agent_key"`
	ServiceInstanceID string `json:"service_instance_id"`
	AdminKey          string `json:"admin_key,omitempty"`
}

// regenerateKeyCancelled is the JSON view of a declined rekey, so a script
// cannot read it as done.
type regenerateKeyCancelled struct {
	jsonHeader
	Cancelled bool `json:"cancelled"`
}

// runConfigRegenerateKey implements `config regenerate-key`: it replaces the
// agent key (and, on request, the administration key) with fresh random
// ones. The rekey is never done for the operator: PRTG sensor URLs embed the
// key and a licence may be bound to it, so the command explains the fallout
// and asks before writing. identity.json is left alone on purpose: the next
// start sees a new instance id and re-records it.
func runConfigRegenerateKey(argv []string, in io.Reader, terminal bool, out, errOut io.Writer) int {
	const command = "config.regenerate-key"
	args, jsonMode := extractJSONFlag(argv)

	var configPathArg string
	withAdminKey, assumeYes := false, false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--admin-key":
			withAdminKey = true
		case args[i] == "--yes":
			assumeYes = true
		case args[i] == "--config-path" && i+1 < len(args):
			configPathArg = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--config-path="):
			configPathArg = strings.TrimPrefix(args[i], "--config-path=")
		default:
			return reportFailure(command, jsonMode, out, fmt.Errorf("unknown argument %q", args[i]))
		}
	}
	configPath, err := cliArgs.GetAbsoluteConfigPath(configPathArg)
	if err != nil {
		return reportFailure(command, jsonMode, out, fmt.Errorf("resolving the configuration path: %w", err))
	}

	newKey, err := configuration.NewAgentKey()
	if err != nil {
		return reportFailure(command, jsonMode, out, err)
	}
	var newAdminKey string
	if withAdminKey {
		newAdminKey, err = configuration.NewAgentKey()
		if err != nil {
			return reportFailure(command, jsonMode, out, err)
		}
	}

	if !jsonMode {
		fmt.Fprintf(out, "A new agent key will be written to %s.\n", configPath)
		fmt.Fprintln(out, "This gives the agent a new identity (a new service.instance.id).")
		fmt.Fprintln(out, "After the replacement:")
		fmt.Fprintln(out, "  - restart the agent service, otherwise it keeps running on the old key;")
		fmt.Fprintln(out, "  - update the PRTG sensor URLs that embed the agent key;")
		fmt.Fprintln(out, "  - have a licence bound to the old key or instance id reissued; a client licence is not affected;")
		fmt.Fprintln(out, "  - in Fleet, this is a new agent: enrol it with a token and revoke or clear the old record.")
	}

	if !assumeYes {
		fmt.Fprint(errOut, "Replace the agent key? [y/N] ")
		ok, interactive := confirmReader(in, terminal, errOut, "--yes")
		if !interactive {
			// Nothing was read and nothing is written; a JSON caller still
			// gets a document saying why.
			return reportFailure(command, jsonMode, out, fmt.Errorf("confirmation required: nothing was changed; rerun with --yes"))
		}
		if !ok {
			if jsonMode {
				return writeCancelled(command, out)
			}
			fmt.Fprintln(out, "Cancelled.")
			return cliexit.OK
		}
	}

	if err := configuration.ReplaceAgentKey(configPath, newKey); err != nil {
		return reportFailure(command, jsonMode, out, err)
	}
	if withAdminKey {
		if err := configuration.ReplaceAdminKey(configPath, newAdminKey); err != nil {
			return reportFailure(command, jsonMode, out, fmt.Errorf(
				"agent.key was replaced (new service.instance.id %s, new key %s) but admin_key was not: %w",
				configuration.AgentInstanceID(newKey), newKey, err))
		}
	}

	instanceID := configuration.AgentInstanceID(newKey)
	if jsonMode {
		return writeJSONRegenerated(command, out, newKey, instanceID, newAdminKey)
	}
	fmt.Fprintln(out, "Agent key replaced.")
	fmt.Fprintf(out, "Agent key: %s\n", newKey)
	fmt.Fprintf(out, "service.instance.id: %s\n", instanceID)
	if withAdminKey {
		fmt.Fprintf(out, "Admin key: %s\n", newAdminKey)
	}
	fmt.Fprintln(out, "Restart the agent service to run under the new key.")
	return cliexit.OK
}

// writeJSONRegenerated prints the JSON view of a done rekey.
func writeJSONRegenerated(command string, out io.Writer, agentKey, instanceID, adminKey string) int {
	doc := regenerateKeyReport{
		jsonHeader:        newJSONHeader(command, cliexit.OK),
		AgentKey:          agentKey,
		ServiceInstanceID: instanceID,
		AdminKey:          adminKey,
	}
	if err := writeJSON(out, doc); err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		return cliexit.Failure
	}
	return cliexit.OK
}

// writeCancelled prints the JSON view of an operator-declined rekey: a
// success-shaped document, because nothing was changed and nothing failed.
func writeCancelled(command string, out io.Writer) int {
	doc := regenerateKeyCancelled{jsonHeader: newJSONHeader(command, cliexit.OK), Cancelled: true}
	if err := writeJSON(out, doc); err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		return cliexit.Failure
	}
	return cliexit.OK
}
