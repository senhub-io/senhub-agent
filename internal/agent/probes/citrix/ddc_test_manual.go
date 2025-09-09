package citrix

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"senhub-agent.go/internal/agent/services/logger"
)

// TestDDCConnectivity tests DDC client with real configuration
func TestDDCConnectivity(ddcURL, username, password string) error {
	// Create simple logger for testing
	baseLogger := &logger.Logger{}
	*baseLogger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	
	// Create DDC configuration
	ddcConfig := DeliveryControllerConfig{
		URL:       ddcURL,
		VerifySSL: false, // For testing with self-signed certs
		Timeout:   30 * time.Second,
	}
	
	// Create auth configuration
	authConfig := AuthConfig{
		Method:   "basic", // or "ntlm"
		Username: username,
		Password: password,
	}
	
	// Create DDC client
	ddcClient, err := NewDeliveryControllerClient(ddcConfig, authConfig, baseLogger)
	if err != nil {
		return fmt.Errorf("failed to create DDC client: %w", err)
	}
	
	ctx := context.Background()
	
	// Test connectivity
	fmt.Println("🔗 Testing DDC connectivity...")
	if err := ddcClient.TestConnectivity(ctx); err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}
	fmt.Println("✅ DDC connectivity successful")
	
	// Test GetSites
	fmt.Println("\n📍 Getting sites...")
	sites, err := ddcClient.GetSites(ctx)
	if err != nil {
		return fmt.Errorf("failed to get sites: %w", err)
	}
	
	fmt.Printf("✅ Found %d sites:\n", len(sites))
	for _, site := range sites {
		fmt.Printf("  - ID: %s, Name: %s\n", site.Id, site.Name)
	}
	
	if len(sites) == 0 {
		fmt.Println("⚠️  No sites found - check DDC configuration")
		return nil
	}
	
	// Test with first site
	siteName := sites[0].Name
	fmt.Printf("\n🖥️  Testing with site: %s\n", siteName)
	
	// Test GetMachinesBySite
	fmt.Println("Getting machine DNS names...")
	machineNames, err := ddcClient.GetMachinesBySite(ctx, siteName)
	if err != nil {
		return fmt.Errorf("failed to get machine names: %w", err)
	}
	fmt.Printf("✅ Found %d machines (DNS names)\n", len(machineNames))
	if len(machineNames) > 0 {
		fmt.Printf("  First machine: %s\n", machineNames[0])
	}
	
	// Test GetMachinesDetailedBySite
	fmt.Println("Getting detailed machines...")
	machines, err := ddcClient.GetMachinesDetailedBySite(ctx, siteName)
	if err != nil {
		return fmt.Errorf("failed to get detailed machines: %w", err)
	}
	fmt.Printf("✅ Found %d detailed machines\n", len(machines))
	if len(machines) > 0 {
		m := machines[0]
		fmt.Printf("  Sample machine:\n")
		fmt.Printf("    ID: %s\n", m.Id)
		fmt.Printf("    Name: %s\n", m.Name)
		fmt.Printf("    DNS: %s\n", m.DNSName)
		fmt.Printf("    Registration: %s\n", m.RegistrationState)
		fmt.Printf("    DeliveryGroup: %s\n", m.DeliveryGroupId)
	}
	
	// Test GetDeliveryGroupsBySite
	fmt.Println("Getting delivery groups...")
	deliveryGroups, err := ddcClient.GetDeliveryGroupsBySite(ctx, siteName)
	if err != nil {
		return fmt.Errorf("failed to get delivery groups: %w", err)
	}
	fmt.Printf("✅ Found %d delivery groups\n", len(deliveryGroups))
	if len(deliveryGroups) > 0 {
		dg := deliveryGroups[0]
		fmt.Printf("  Sample delivery group:\n")
		fmt.Printf("    ID: %s\n", dg.Id)
		fmt.Printf("    Name: %s\n", dg.Name)
		fmt.Printf("    Total Machines: %d\n", dg.TotalMachines)
		fmt.Printf("    Enabled: %t\n", dg.Enabled)
	}
	
	// Test GetSiteDetails
	fmt.Println("Getting site details...")
	siteDetails, err := ddcClient.GetSiteDetails(ctx, siteName)
	if err != nil {
		return fmt.Errorf("failed to get site details: %w", err)
	}
	fmt.Printf("✅ Site details:\n")
	fmt.Printf("  Total Machines: %d\n", siteDetails.TotalMachines)
	fmt.Printf("  Registered Machines: %d\n", siteDetails.RegisteredMachines)
	fmt.Printf("  Active Sessions: %d\n", siteDetails.ActiveSessions)
	fmt.Printf("  Delivery Groups: %d\n", len(siteDetails.DeliveryGroups))
	fmt.Printf("  Controllers: %d\n", len(siteDetails.Controllers))
	
	fmt.Println("\n🎉 All DDC tests passed!")
	return nil
}

// TestInventoryService tests the site inventory service
func TestInventoryService(ddcURL, username, password, siteName string) error {
	// Create simple logger for testing
	baseLogger := &logger.Logger{}
	*baseLogger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	
	// Create DDC configuration
	ddcConfig := DeliveryControllerConfig{
		URL:       ddcURL,
		VerifySSL: false,
		Timeout:   30 * time.Second,
	}
	
	// Create auth configuration
	authConfig := AuthConfig{
		Method:   "basic",
		Username: username,
		Password: password,
	}
	
	// Create DDC client
	ddcClient, err := NewDeliveryControllerClient(ddcConfig, authConfig, baseLogger)
	if err != nil {
		return fmt.Errorf("failed to create DDC client: %w", err)
	}
	
	// Create inventory service
	inventoryService := NewInventoryService(ddcClient, 5*time.Minute, baseLogger)
	
	ctx := context.Background()
	
	fmt.Printf("🗂️  Testing inventory service with site: %s\n", siteName)
	
	// Test inventory refresh
	if err := inventoryService.RefreshInventory(ctx, siteName); err != nil {
		return fmt.Errorf("failed to refresh inventory: %w", err)
	}
	
	// Test inventory methods
	fmt.Println("Testing inventory filters...")
	
	// Get some machines to test with
	machineNames := inventoryService.GetMachinesForSite()
	if len(machineNames) > 0 {
		testMachine := machineNames[0]
		isInSite := inventoryService.IsInSite(testMachine)
		fmt.Printf("  Machine %s in site: %t\n", testMachine, isInSite)
	}
	
	// Get inventory stats
	stats := inventoryService.GetInventoryStats()
	fmt.Printf("📊 Inventory stats:\n")
	for key, value := range stats {
		fmt.Printf("  %s: %v\n", key, value)
	}
	
	fmt.Println("\n🎉 Inventory service test passed!")
	return nil
}