package app

import (
	"fmt"
	"os"
	"time"

	"github.com/alexflint/go-arg"
	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/license"
)

// handleLicenseCommand handles the license subcommand
func handleLicenseCommand() {
	// Parse license subcommand args
	type LicenseCmd struct {
		Activate *cliArgs.LicenseActivateArgs `arg:"subcommand:activate"`
		Show     *cliArgs.LicenseShowArgs     `arg:"subcommand:show"`
		Remove   *cliArgs.LicenseRemoveArgs   `arg:"subcommand:remove"`
	}

	var cmd LicenseCmd
	parser, err := arg.NewParser(arg.Config{Program: "agent license"}, &cmd)
	if err != nil {
		fatalf("failed to create parser: %v", err)
	}

	// Parse args starting from index 2 (skip "agent" and "license")
	err = parser.Parse(os.Args[2:])
	if err != nil {
		if err == arg.ErrHelp {
			parser.WriteHelp(os.Stdout)
			os.Exit(0)
		}
		// A parse error is a diagnostic, not data: route the cause and the
		// usage to stderr with the unified "Error:" prefix so piped callers
		// see the failure rather than receiving usage text on stdout.
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		parser.WriteUsage(os.Stderr)
		os.Exit(1)
	}

	// Handle subcommands
	switch {
	case cmd.Activate != nil:
		handleLicenseActivate(cmd.Activate)
	case cmd.Show != nil:
		handleLicenseShow(cmd.Show)
	case cmd.Remove != nil:
		handleLicenseRemove(cmd.Remove)
	default:
		parser.WriteHelp(os.Stdout)
		os.Exit(1)
	}
}

// handleLicenseActivate activates a license
func handleLicenseActivate(args *cliArgs.LicenseActivateArgs) {
	// Get absolute config path
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}

	// Validate license code with RSA signature verification
	validator, err := license.GetDefaultValidator(7)
	if err != nil {
		fatalf("failed to initialize license validator: %v", err)
	}

	validatedLicense, err := validator.ValidateLicense(args.LicenseCode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid license code: %v\n", err)
		os.Exit(1)
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

	// Persist the license with a node-level edit that preserves the file's
	// layout. A full unmarshal/marshal of LocalConfigurationData would re-emit
	// empty probes:/storage: blocks and flip a multi-file install back to legacy
	// monolithic, silently dropping probes.d/ + strategies.d/.
	if err := configuration.SetLicenseField(configPath, args.LicenseCode); err != nil {
		fatalf("failed to write license to config: %v", err)
	}

	fmt.Printf("\nLicense activated and saved to: %s\n", configPath)
	fmt.Println("   Restart the agent for changes to take effect.")
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

	// Check if license exists
	if config.Agent.License == "" {
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

	validatedLicense, err := validator.ValidateLicense(config.Agent.License)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid license in config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Current License Information")
	fmt.Println("================================")
	fmt.Printf("Tier: %s\n", validatedLicense.Tier)
	fmt.Printf("Subject: %s\n", validatedLicense.Subject)
	fmt.Printf("Issued at: %s\n", validatedLicense.IssuedAt.Format(time.RFC1123))
	fmt.Printf("Expires at: %s\n", validatedLicense.ExpiresAt.Format(time.RFC1123))

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

// handleLicenseRemove removes the current license
func handleLicenseRemove(args *cliArgs.LicenseRemoveArgs) {
	// Get absolute config path
	configPath, err := cliArgs.GetAbsoluteConfigPath(args.ConfigPath)
	if err != nil {
		fatalf("failed to determine config path: %v", err)
	}

	// Confirm if not forced. Reuse the shared confirmation helper so this
	// destructive prompt behaves identically to uninstall / secret rm
	// (whitespace-tolerant, non-TTY stdin aborts).
	if !args.Force {
		fmt.Print("Are you sure you want to remove the license? [y/N] ")
		if !readYesConfirmation() {
			fmt.Println("Cancelled.")
			return
		}
	}

	// Clear the license with the same node-level edit as activate, so removing a
	// license on a multi-file install does not corrupt the layout.
	if err := configuration.SetLicenseField(configPath, ""); err != nil {
		fatalf("failed to write license to config: %v", err)
	}

	fmt.Printf("License removed from: %s\n", configPath)
	fmt.Println("   Agent will run in free tier mode.")
	fmt.Println("   Restart the agent for changes to take effect.")
}
