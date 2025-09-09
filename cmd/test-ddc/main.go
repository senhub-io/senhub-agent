package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/probes/citrix"
	"senhub-agent.go/internal/agent/services/logger"
)

func main() {
	// Parse command line flags
	ddcURL := flag.String("url", "", "Delivery Controller URL (required)")
	username := flag.String("username", "", "Username for authentication (required)")
	password := flag.String("password", "", "Password for authentication (required)")
	siteName := flag.String("site", "", "Site name to test (if empty, uses first found)")
	authMethod := flag.String("auth", "basic", "Authentication method (basic or ntlm)")
	skipSSL := flag.Bool("skip-ssl", true, "Skip SSL certificate verification")
	testInventory := flag.Bool("inventory", false, "Test inventory service")
	verbose := flag.Bool("verbose", false, "Verbose logging")
	
	flag.Parse()

	// Validate required parameters
	if *ddcURL == "" || *username == "" || *password == "" {
		fmt.Println("❌ Missing required parameters")
		flag.Usage()
		os.Exit(1)
	}

	// Setup logger
	level := zerolog.InfoLevel
	if *verbose {
		level = zerolog.DebugLevel
	}
	baseLogger := zerolog.New(os.Stdout).Level(level).With().Timestamp().Logger()

	fmt.Println("🧪 SenHub Agent - DDC Test Tool")
	fmt.Printf("URL: %s\n", *ddcURL)
	fmt.Printf("Username: %s\n", *username)
	fmt.Printf("Auth Method: %s\n", *authMethod)
	fmt.Printf("Skip SSL: %t\n", *skipSSL)
	fmt.Println("=====================================")

	// Create DDC client directly (simpler than using test functions)
	ddcConfig := citrix.DeliveryControllerConfig{
		URL:       *ddcURL,
		VerifySSL: !*skipSSL,
		Timeout:   30 * time.Second,
	}
	
	authConfig := citrix.AuthConfig{
		Method:   *authMethod,
		Username: *username,
		Password: *password,
	}
	
	ddcClient, err := citrix.NewDeliveryControllerClient(ddcConfig, authConfig, &baseLogger)
	if err != nil {
		fmt.Printf("❌ Failed to create DDC client: %v\n", err)
		os.Exit(1)
	}
	
	ctx := context.Background()
	
	// Step 1: Test connectivity
	fmt.Println("\n🔗 Step 1: Testing DDC connectivity...")
	if err := ddcClient.TestConnectivity(ctx); err != nil {
		fmt.Printf("❌ DDC connectivity failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ DDC connectivity successful")
	
	// Step 2: Get sites
	fmt.Println("\n📍 Step 2: Getting sites...")
	sites, err := ddcClient.GetSites(ctx)
	if err != nil {
		fmt.Printf("❌ Failed to get sites: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Found %d sites:\n", len(sites))
	for i, site := range sites {
		fmt.Printf("  %d. ID: %s, Name: %s\n", i+1, site.Id, site.Name)
	}
	
	if len(sites) == 0 {
		fmt.Println("⚠️  No sites found - check DDC configuration")
		os.Exit(0)
	}
	
	// Determine which site to test
	testSite := *siteName
	if testSite == "" {
		testSite = sites[0].Name
		fmt.Printf("Using first site: %s\n", testSite)
	}
	
	// Step 3: Test machines
	fmt.Printf("\n🖥️  Step 3: Testing machines for site '%s'...\n", testSite)
	machines, err := ddcClient.GetMachinesDetailedBySite(ctx, testSite)
	if err != nil {
		fmt.Printf("❌ Failed to get machines: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Found %d machines\n", len(machines))
	if len(machines) > 0 {
		m := machines[0]
		fmt.Printf("  Sample machine:\n")
		fmt.Printf("    Name: %s\n", m.Name)
		fmt.Printf("    DNS: %s\n", m.DNSName)
		fmt.Printf("    Registration: %s\n", m.RegistrationState)
		fmt.Printf("    Power State: %s\n", m.PowerState)
		fmt.Printf("    Session Count: %d\n", m.SessionCount)
	}
	
	// Step 4: Test delivery groups
	fmt.Printf("\n📦 Step 4: Testing delivery groups for site '%s'...\n", testSite)
	deliveryGroups, err := ddcClient.GetDeliveryGroupsBySite(ctx, testSite)
	if err != nil {
		fmt.Printf("❌ Failed to get delivery groups: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Found %d delivery groups\n", len(deliveryGroups))
	if len(deliveryGroups) > 0 {
		dg := deliveryGroups[0]
		fmt.Printf("  Sample delivery group:\n")
		fmt.Printf("    Name: %s\n", dg.Name)
		fmt.Printf("    Total Machines: %d\n", dg.TotalMachines)
		fmt.Printf("    Assigned Machines: %d\n", dg.TotalAssignedMachines)
		fmt.Printf("    Enabled: %t\n", dg.Enabled)
	}
	
	// Step 5: Test site details
	fmt.Printf("\n📊 Step 5: Getting site details for '%s'...\n", testSite)
	siteDetails, err := ddcClient.GetSiteDetails(ctx, testSite)
	if err != nil {
		fmt.Printf("❌ Failed to get site details: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Site '%s' details:\n", testSite)
	fmt.Printf("  Total Machines: %d\n", siteDetails.TotalMachines)
	fmt.Printf("  Registered Machines: %d\n", siteDetails.RegisteredMachines)
	fmt.Printf("  Active Sessions: %d\n", siteDetails.ActiveSessions)
	fmt.Printf("  Delivery Groups: %v\n", siteDetails.DeliveryGroups)
	fmt.Printf("  Controllers: %v\n", siteDetails.Controllers)
	
	// Optional: Test inventory service
	if *testInventory {
		fmt.Printf("\n🗂️  Step 6: Testing inventory service for '%s'...\n", testSite)
		inventoryService := citrix.NewInventoryService(ddcClient, 5*time.Minute, &baseLogger)
		
		if err := inventoryService.RefreshInventory(ctx, testSite); err != nil {
			fmt.Printf("❌ Failed to refresh inventory: %v\n", err)
			os.Exit(1)
		}
		
		stats := inventoryService.GetInventoryStats()
		fmt.Printf("✅ Inventory loaded:\n")
		for key, value := range stats {
			switch v := value.(type) {
			case time.Time:
				fmt.Printf("  %s: %s\n", key, v.Format("2006-01-02 15:04:05"))
			case time.Duration:
				fmt.Printf("  %s: %s\n", key, v.String())
			default:
				fmt.Printf("  %s: %v\n", key, value)
			}
		}
		
		// Test filtering
		machines := inventoryService.GetMachinesForSite()
		if len(machines) > 0 {
			testMachine := machines[0]
			isInSite := inventoryService.IsInSite(testMachine)
			fmt.Printf("  Test filter - Machine %s in site: %t\n", testMachine, isInSite)
		}
	}
	
	fmt.Println("\n🎉 All DDC tests completed successfully!")
	fmt.Printf("Site '%s' is ready for filtering with %d machines\n", testSite, siteDetails.TotalMachines)
}