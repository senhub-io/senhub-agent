package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"golang.org/x/term"
	"gopkg.in/yaml.v2"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
	"senhub-agent.go/internal/cliexit"
)

// handleLicenseCommand handles the license subcommand
func handleLicenseCommand() {
	// Parse license subcommand args
	type LicenseCmd struct {
		Activate    *cliArgs.LicenseActivateArgs    `arg:"subcommand:activate"`
		Show        *cliArgs.LicenseShowArgs        `arg:"subcommand:show"`
		Key         *cliArgs.LicenseShowArgs        `arg:"subcommand:key"`
		Remove      *cliArgs.LicenseRemoveArgs      `arg:"subcommand:remove"`
		Revocations *cliArgs.LicenseRevocationsArgs `arg:"subcommand:revocations"`
	}

	var cmd LicenseCmd
	parser, err := arg.NewParser(arg.Config{Program: "agent license"}, &cmd)
	if err != nil {
		fatalf("failed to create parser: %v", err)
	}

	// Parse args starting from index 2 (skip "agent" and "license")
	err = parser.Parse(os.Args[2:])
	if err != nil {
		if errors.Is(err, arg.ErrHelp) {
			parser.WriteHelp(os.Stdout)
			os.Exit(cliexit.OK)
		}
		// A parse error is a diagnostic, not data: route the cause and the
		// usage to stderr with the unified "Error:" prefix so piped callers
		// see the failure rather than receiving usage text on stdout.
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		parser.WriteUsage(os.Stderr)
		os.Exit(cliexit.Failure)
	}

	// Handle subcommands
	switch {
	case cmd.Activate != nil:
		handleLicenseActivate(cmd.Activate)
	case cmd.Show != nil:
		handleLicenseShow(cmd.Show)
	case cmd.Key != nil:
		handleLicenseKey(cmd.Key)
	case cmd.Remove != nil:
		handleLicenseRemove(cmd.Remove)
	case cmd.Revocations != nil && cmd.Revocations.Import != nil:
		handleLicenseRevocationsImport(cmd.Revocations.Import)
	default:
		parser.WriteHelp(os.Stdout)
		os.Exit(cliexit.Failure)
	}
}

// handleLicenseActivate activates a license
// handleLicenseKey prints this agent's key, which a licence must be
// bound to. It is the value to hand to Sensor Factory when ordering a
// paid-tier licence, and the value `license activate` checks against.
func handleLicenseKey(args *cliArgs.LicenseShowArgs) {
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}
	agentKey, err := extractAgentKeyFromConfig(configPath)
	if err != nil {
		fatalf("failed to read the agent key: %v", err)
	}
	if agentKey == "" {
		fatalf("no agent key in %s", configPath)
	}
	fmt.Println(agentKey)
}

func handleLicenseActivate(args *cliArgs.LicenseActivateArgs) {
	// Get absolute config path
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}

	// The Fleet server is the single licence writer under
	// opamp.license_from_server; a local write would race it.
	if refuseWhenLicenceFromServer(configPath, os.Stderr) {
		os.Exit(cliexit.Failure)
	}

	// Validate license code with RSA signature verification
	validator, err := license.GetDefaultValidator(7)
	if err != nil {
		fatalf("failed to initialize license validator: %v", err)
	}

	code, err := licenseCodeFrom(args.LicenseCode, os.Stdin)
	if err != nil {
		fatalf("%v", err)
	}
	args.LicenseCode = code

	validatedLicense, err := validator.ValidateLicense(args.LicenseCode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid license code: %v\n", err)
		os.Exit(cliexit.Failure)
	}

	// Show license information
	fmt.Println("License validated successfully")
	fmt.Printf("   Tier: %s\n", validatedLicense.Tier)
	fmt.Printf("   Authorized probes: %v\n", validatedLicense.AuthorizedProbes)
	fmt.Printf("   Expires at: %s\n", validatedLicense.ExpiresAt.Format(time.RFC1123))

	if validatedLicense.IsExpired {
		fmt.Printf("   License is expired\n")
		if validator.IsInGracePeriod(validatedLicense) {
			gracePeriodEnd := validatedLicense.ExpiresAt.Add(time.Duration(validatedLicense.GracePeriodDays) * 24 * time.Hour)
			fmt.Printf("   Grace period active until: %s\n", gracePeriodEnd.Format(time.RFC1123))
		} else {
			fmt.Printf("   Grace period has ended - license is inactive\n")
		}
	}

	// A licence binds to one machine through its Subject claim, which is
	// the agent key. Activating a licence minted for another agent key
	// writes a file the agent then refuses at boot (the same VerifyBinding
	// runs there), leaving the host silently on the Free tier. Catch the
	// mismatch here, where the operator is watching, and refuse rather
	// than persist a licence that will never take effect.
	agentKey, keyErr := extractAgentKeyFromConfig(configPath)
	if keyErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read this agent's key to verify the licence: %v\n", keyErr)
	} else if !license.VerifyBinding("", agentKey, validatedLicense) {
		// Only reachable for a per-agent licence (a UUID subject) issued
		// for a different agent; a customer or unbound licence is accepted.
		fmt.Fprintf(os.Stderr, "Error: this licence is issued for another agent (%q); this agent is %q.\n", validatedLicense.Subject, agentKey)
		fmt.Fprintf(os.Stderr, "Use your customer licence, or one issued for this agent.\n")
		os.Exit(cliexit.Failure)
	} else {
		switch {
		case validatedLicense.Subject == "":
			fmt.Println("   Scope: valid on any agent")
		case license.SubjectIsAgentKey(validatedLicense.Subject):
			fmt.Println("   Scope: bound to this agent")
		default:
			fmt.Printf("   Scope: customer licence (%s), valid fleet-wide\n", validatedLicense.Subject)
		}
	}

	// Persist the license to a dedicated sidecar file (license.jwt) next to the
	// config, and clear any inline agent.license so the sidecar is the single
	// source of truth. A standalone file is trivial to hand to an operator and
	// avoids mangling a long JWT on copy-paste into YAML; the loader picks it up
	// automatically when the inline field is empty.
	if err := configuration.WriteLicenseSidecar(configPath, args.LicenseCode); err != nil {
		fatalf("failed to write license: %v", err)
	}

	fmt.Printf("\nLicense activated and saved to: %s\n", configuration.LicenseSidecarPath(configPath))
	fmt.Println("   A running agent applies it within a few seconds; restart it only if 'agent doctor' reports the configuration watch off.")
}

// handleLicenseShow shows current license information
func handleLicenseShow(args *cliArgs.LicenseShowArgs) {
	// Get absolute config path
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}

	// Load configuration file
	data, err := os.ReadFile(configPath)
	if err != nil {
		fatalf("failed to read config file %s: %v", configPath, err)
	}

	var config configuration.LocalConfigurationData
	if err := yaml.Unmarshal(data, &config); err != nil {
		fatalf("failed to parse config file: %v", err)
	}

	// Resolve the effective license the same way the loader does at boot: the
	// inline agent.license (with ${...} substitution) if set, otherwise the
	// license.jwt sidecar next to the config. This keeps `show` truthful whether
	// the license lives inline or in the sidecar file.
	effectiveLicense, err := configuration.ResolveEffectiveLicense(configPath, config.Agent.License)
	if err != nil {
		fatalf("failed to resolve license: %v", err)
	}

	// Check if license exists
	if effectiveLicense == "" {
		fmt.Println("No license configured (Free tier)")
		fmt.Println("\nFree tier probes:")
		for _, probe := range license.GetFreeTierProbes() {
			fmt.Printf("  - %s\n", probe)
		}
		return
	}

	// Validate and show license with RSA signature verification
	validator, err := license.GetDefaultValidator(7)
	if err != nil {
		fatalf("failed to initialize license validator: %v", err)
	}

	validatedLicense, err := validator.ValidateLicense(effectiveLicense)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid license in config: %v\n", err)
		os.Exit(cliexit.Failure)
	}

	fmt.Println("Current License Information")
	fmt.Println("================================")
	fmt.Printf("Tier: %s\n", validatedLicense.Tier)
	fmt.Printf("Subject: %s\n", validatedLicense.Subject)
	fmt.Printf("Issued at: %s\n", validatedLicense.IssuedAt.Format(time.RFC1123))
	fmt.Printf("Expires at: %s\n", validatedLicense.ExpiresAt.Format(time.RFC1123))
	printLicenseSigner(os.Stdout, validatedLicense)

	if validatedLicense.IsExpired {
		fmt.Printf("\nStatus: EXPIRED\n")
		if validator.IsInGracePeriod(validatedLicense) {
			gracePeriodEnd := validatedLicense.ExpiresAt.Add(time.Duration(validatedLicense.GracePeriodDays) * 24 * time.Hour)
			fmt.Printf("Grace period active until: %s\n", gracePeriodEnd.Format(time.RFC1123))
		} else {
			fmt.Printf("Grace period ended - license is inactive\n")
		}
	} else {
		daysUntilExpiry := int(time.Until(validatedLicense.ExpiresAt).Hours() / 24)
		fmt.Printf("\nStatus: ACTIVE (%d days remaining)\n", daysUntilExpiry)
	}

	fmt.Println("\nAuthorized probes:")
	for _, probe := range validatedLicense.AuthorizedProbes {
		if probe == "*" {
			fmt.Println("  - * (all probes)")
		} else {
			fmt.Printf("  - %s\n", probe)
		}
	}
}

// confirmLicenseRemove asks the operator to confirm the destructive
// removal. A non-terminal stdin is an error, not a "no": an automation
// must never read a removal that did not happen as success.
func confirmLicenseRemove(in *os.File, out, errOut io.Writer) (proceed bool, code int) {
	return confirmLicenseRemoveFrom(in, term.IsTerminal(int(in.Fd())), out, errOut)
}

func confirmLicenseRemoveFrom(in io.Reader, terminal bool, out, errOut io.Writer) (proceed bool, code int) {
	fmt.Fprint(out, "Are you sure you want to remove the license? [y/N] ")
	ok, interactive := confirmReader(in, terminal, errOut, "--force")
	if ok {
		return true, cliexit.OK
	}
	if !interactive {
		fmt.Fprintln(errOut, "Error: confirmation required: nothing was removed; rerun with --force.")
		return false, cliexit.Failure
	}
	fmt.Fprintln(out, "Cancelled.")
	return false, cliexit.OK
}

// handleLicenseRemove removes the current license
func handleLicenseRemove(args *cliArgs.LicenseRemoveArgs) {
	// Get absolute config path
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}

	// Same single-writer rule as activate: Fleet owns the licence here.
	if refuseWhenLicenceFromServer(configPath, os.Stderr) {
		os.Exit(cliexit.Failure)
	}

	// Confirm unless forced; without a terminal and without --force the
	// command fails instead of looking like a user's "no", as uninstall
	// and secret rm do.
	if !args.Force {
		if proceed, code := confirmLicenseRemove(os.Stdin, os.Stdout, os.Stderr); !proceed {
			if code != cliexit.OK {
				os.Exit(code)
			}
			return
		}
	}

	// Delete the license sidecar and clear any inline agent.license (the
	// node-level edit preserves a multi-file layout, so removing a license does
	// not corrupt probes.d/ + strategies.d/).
	if err := configuration.RemoveLicenseSidecar(configPath); err != nil {
		fatalf("failed to remove license: %v", err)
	}

	fmt.Printf("License removed from: %s\n", configPath)
	fmt.Println("   Agent will run in free tier mode.")
	fmt.Println("   A running agent applies it within a few seconds; restart it only if 'agent doctor' reports the configuration watch off.")
}

// licenseCodeFrom returns the licence given on the command line, or reads
// it from stdin when the argument is "-" or absent and stdin is not a
// terminal. On the command line the token is visible to every process on
// the machine and stays in the shell history; the secret command already
// refuses values there for that reason.
func licenseCodeFrom(arg string, stdin *os.File) (string, error) {
	if arg != "" && arg != "-" {
		return arg, nil
	}
	if arg == "" && term.IsTerminal(int(stdin.Fd())) {
		return "", errors.New("no license code: pass it on standard input (senhub-agent license activate - < license.jwt) or as the argument")
	}
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return "", fmt.Errorf("reading the license code from standard input: %w", err)
	}
	code := strings.TrimSpace(string(data))
	if code == "" {
		return "", errors.New("no license code on standard input")
	}
	return code, nil
}

// refuseWhenLicenceFromServer reports whether a local licence write must be
// refused: the Fleet server owns the licence, or agent.yaml cannot be read
// to tell.
func refuseWhenLicenceFromServer(configPath string, errOut io.Writer) bool {
	flag, err := configuration.LicenceFromServer(configPath)
	if err != nil {
		// Unknown means the server may own the licence: refuse rather
		// than risk a second writer.
		fmt.Fprintf(errOut, "Error: cannot tell whether the Fleet server owns the licence (opamp.license_from_server in %s): %v\n", configPath, err)
		return true
	}
	if !flag {
		return false
	}
	fmt.Fprintf(errOut, "Error: this agent receives its licence from the Fleet server (opamp.license_from_server: true in %s); a local licence would be a second writer. Set license_from_server to false first, or manage the licence in Fleet.\n", configPath)
	return true
}

// handleLicenseRevocationsImport imports a signed revocation list without
// contacting the server (issue #149): the token travels out of band.
func handleLicenseRevocationsImport(args *cliArgs.LicenseRevocationsImportArgs) {
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}

	if refuseWhenLicenceFromServer(configPath, os.Stderr) {
		os.Exit(cliexit.Failure)
	}

	token, err := licenseCodeFrom(args.Token, os.Stdin)
	if err != nil {
		fatalf("%v", err)
	}

	validator, err := license.GetDefaultValidator(7)
	if err != nil {
		fatalf("failed to initialize license validator: %v", err)
	}

	os.Exit(runLicenseRevocationsImport(validator, configPath, token, os.Stdout, os.Stderr))
}

// runLicenseRevocationsImport validates the incoming list, compares it with
// the sidecar (an unreadable or invalid held list counts as absent), then
// writes it. Returns the exit code so tests can drive it.
func runLicenseRevocationsImport(v *license.JWTValidator, configPath, token string, out, errOut io.Writer) int {
	list, err := v.ValidateRevocationList(token)
	if err != nil {
		fmt.Fprintf(errOut, "Error: invalid revocation list: %v\n", err)
		return cliexit.Failure
	}

	heldToken, held := heldRevocations(v, configPath)
	if held != nil && heldToken == token && held.Version == list.Version {
		fmt.Fprintf(out, "Revocation list version %d is already in place.\n", list.Version)
		return cliexit.OK
	}
	if held != nil {
		if err := license.CheckNewer(list, held); err != nil {
			fmt.Fprintf(errOut, "Error: revocation list version %d is not newer than the version %d held.\n", list.Version, held.Version)
			return cliexit.Failure
		}
	}

	if err := configuration.WriteRevocationsSidecar(configPath, token); err != nil {
		fmt.Fprintf(errOut, "Error: failed to write the revocation list: %v\n", err)
		return cliexit.Failure
	}

	fmt.Fprintf(out, "Revocation list version %d imported (%d revoked licences) to %s.\n", list.Version, len(list.Revoked), configuration.RevocationsSidecarPath(configPath))

	if jti := revokedJtiInForce(v, configPath, list); jti != "" {
		fmt.Fprintf(out, "   The licence in force (jti %s) is revoked: the agent drops to the Free tier within a few seconds.\n", jti)
	}

	fmt.Fprintln(out, "   A running agent applies the list within a few seconds; restart it only if 'agent doctor' reports the configuration watch off.")
	return cliexit.OK
}

// heldRevocations returns the sidecar token and its validated list; any
// read or validation failure means "no list held".
func heldRevocations(v *license.JWTValidator, configPath string) (string, *license.RevocationList) {
	heldToken, err := configuration.ReadRevocationsSidecar(configPath)
	if err != nil || heldToken == "" {
		return "", nil
	}
	held, err := v.ValidateRevocationList(heldToken)
	if err != nil {
		return "", nil
	}
	return heldToken, held
}

// revokedJtiInForce returns the jti of the effective licence when the new
// list revokes it, so the operator learns the pending downgrade immediately.
func revokedJtiInForce(v *license.JWTValidator, configPath string, list *license.RevocationList) string {
	licToken, err := configuration.ResolveEffectiveLicense(configPath, "")
	if err != nil || licToken == "" {
		return ""
	}
	lic, err := v.ValidateLicense(licToken)
	if err != nil || !list.Revokes(lic.ID) {
		return ""
	}
	return lic.ID
}

// printLicenseSigner tells who signed the licence: the root key, or a
// delegating platform with its key id and the date its delegation ends.
func printLicenseSigner(out io.Writer, lic *license.License) {
	if lic.Platform == "" {
		fmt.Fprintln(out, "Signed by: the root key")
		return
	}
	fmt.Fprintf(out, "Signed by: platform %s (delegation)\n", lic.Platform)
	fmt.Fprintf(out, "Delegation kid: %s\n", lic.DelegationKID)
	if !lic.DelegationExpiresAt.IsZero() {
		fmt.Fprintf(out, "Delegation expires at: %s\n", lic.DelegationExpiresAt.Format(time.RFC1123))
	}
}
