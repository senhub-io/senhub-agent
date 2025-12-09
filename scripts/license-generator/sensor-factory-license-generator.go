package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// PRODUCTION LICENSE GENERATOR FOR SENSOR FACTORY
//
// This script is designed to be integrated into Sensor Factory for production license management.
//
// SECURITY REQUIREMENTS:
// - Private key MUST be stored in a secure vault (HashiCorp Vault, AWS Secrets Manager, etc.)
// - Private key should NEVER be committed to version control
// - Public key should be embedded in agent binary (internal/agent/services/license/public_key.go)
//
// USAGE:
// 1. ONE-TIME: Generate production RSA key pair
//    go run sensor-factory-license-generator.go --generate-keys
//
// 2. ONGOING: Generate customer licenses
//    go run sensor-factory-license-generator.go --generate-license \
//      --customer-id "customer-name" \
//      --tier "pro" \
//      --probes "redfish,citrix,syslog" \
//      --validity-days 365

func main() {
	fmt.Println("🏭 SenHub Sensor Factory - Production License Generator")
	fmt.Println("========================================================")
	fmt.Println()

	// Check command line arguments
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "--generate-keys", "-k":
		generateProductionKeys()
	case "--generate-license", "-l":
		generateCustomerLicense()
	case "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println()
	fmt.Println("  Generate production RSA key pair (ONE-TIME OPERATION):")
	fmt.Println("    go run sensor-factory-license-generator.go --generate-keys")
	fmt.Println()
	fmt.Println("  Generate customer license:")
	fmt.Println("    go run sensor-factory-license-generator.go --generate-license \\")
	fmt.Println("      --customer-id <customer-name> \\")
	fmt.Println("      --tier <free|pro|enterprise> \\")
	fmt.Println("      --probes <probe1,probe2,...> \\")
	fmt.Println("      --validity-days <days>")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println()
	fmt.Println("  # Generate Pro license for Noble Age with redfish and citrix")
	fmt.Println("  go run sensor-factory-license-generator.go --generate-license \\")
	fmt.Println("    --customer-id \"noble-age\" \\")
	fmt.Println("    --tier \"pro\" \\")
	fmt.Println("    --probes \"redfish,citrix\" \\")
	fmt.Println("    --validity-days 365")
	fmt.Println()
	fmt.Println("  # Generate Enterprise license with all probes")
	fmt.Println("  go run sensor-factory-license-generator.go --generate-license \\")
	fmt.Println("    --customer-id \"enterprise-customer\" \\")
	fmt.Println("    --tier \"enterprise\" \\")
	fmt.Println("    --probes \"*\" \\")
	fmt.Println("    --validity-days 730")
}

// generateProductionKeys generates RSA key pair for production use
// This should be executed ONCE in a secure environment
func generateProductionKeys() {
	fmt.Println("🔑 Generating PRODUCTION RSA key pair (4096-bit)...")
	fmt.Println()
	fmt.Println("⚠️  SECURITY WARNING:")
	fmt.Println("   - Private key will be saved as 'senhub-license-private-key.pem'")
	fmt.Println("   - Store this file in a SECURE VAULT immediately")
	fmt.Println("   - DO NOT commit this file to version control")
	fmt.Println("   - Public key should be embedded in agent binary")
	fmt.Println()

	// Confirm generation
	fmt.Print("Continue? (yes/no): ")
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	// Generate RSA private key (4096 bits for production security)
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		panic(err)
	}

	// Encode private key to PEM format
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	// Encode public key to PEM format
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		panic(err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	// Save private key with restricted permissions
	err = os.WriteFile("senhub-license-private-key.pem", privateKeyPEM, 0600)
	if err != nil {
		panic(err)
	}

	// Save public key
	err = os.WriteFile("senhub-license-public-key.pem", publicKeyPEM, 0644)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ Production keys generated successfully!")
	fmt.Println()
	fmt.Println("📄 Files created:")
	fmt.Println("   🔐 senhub-license-private-key.pem (KEEP SECRET - for Sensor Factory only)")
	fmt.Println("   📖 senhub-license-public-key.pem (embed in agent binary)")
	fmt.Println()
	fmt.Println("📋 Next steps:")
	fmt.Println("   1. Move private key to secure vault immediately")
	fmt.Println("   2. Update agent's internal/agent/services/license/public_key.go with public key content")
	fmt.Println("   3. Delete senhub-license-private-key.pem from disk after securing in vault")
	fmt.Println()
	fmt.Println("📝 Public key content for agent:")
	fmt.Println("   Copy the following into public_key.go:")
	fmt.Println()
	fmt.Printf("%s", string(publicKeyPEM))
}

// generateCustomerLicense generates a JWT license for a customer
// This is the main function used in production to create licenses
func generateCustomerLicense() {
	// Parse command line arguments
	var customerID, tier, probesStr string
	var validityDays int

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--customer-id":
			if i+1 < len(os.Args) {
				customerID = os.Args[i+1]
				i++
			}
		case "--tier":
			if i+1 < len(os.Args) {
				tier = os.Args[i+1]
				i++
			}
		case "--probes":
			if i+1 < len(os.Args) {
				probesStr = os.Args[i+1]
				i++
			}
		case "--validity-days":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &validityDays)
				i++
			}
		}
	}

	// Validate parameters
	if customerID == "" || tier == "" || probesStr == "" || validityDays == 0 {
		fmt.Println("❌ Error: Missing required parameters")
		fmt.Println()
		printUsage()
		os.Exit(1)
	}

	if tier != "free" && tier != "pro" && tier != "enterprise" {
		fmt.Println("❌ Error: Invalid tier. Must be 'free', 'pro', or 'enterprise'")
		os.Exit(1)
	}

	// Parse probes list
	var probes []string
	if probesStr == "*" {
		probes = []string{"*"}
	} else {
		// Split comma-separated probe list
		probes = []string{}
		currentProbe := ""
		for _, char := range probesStr {
			if char == ',' {
				if currentProbe != "" {
					probes = append(probes, currentProbe)
					currentProbe = ""
				}
			} else {
				currentProbe += string(char)
			}
		}
		if currentProbe != "" {
			probes = append(probes, currentProbe)
		}
	}

	// Load private key
	fmt.Println("🔑 Loading private key from senhub-license-private-key.pem...")
	privateKeyPEM, err := os.ReadFile("senhub-license-private-key.pem")
	if err != nil {
		fmt.Printf("❌ Error: Cannot read private key file: %v\n", err)
		fmt.Println("   Make sure senhub-license-private-key.pem exists in current directory")
		fmt.Println("   Or run with --generate-keys first to create the key pair")
		os.Exit(1)
	}

	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		fmt.Println("❌ Error: Failed to parse private key PEM")
		os.Exit(1)
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		fmt.Printf("❌ Error: Failed to parse private key: %v\n", err)
		os.Exit(1)
	}

	// Generate license
	fmt.Println()
	fmt.Println("📜 Generating license...")
	fmt.Printf("   Customer: %s\n", customerID)
	fmt.Printf("   Tier: %s\n", tier)
	fmt.Printf("   Probes: %v\n", probes)
	fmt.Printf("   Validity: %d days\n", validityDays)
	fmt.Println()

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(time.Duration(validityDays) * 24 * time.Hour)

	claims := jwt.MapClaims{
		"tier":              tier,
		"authorized_probes": probes,
		"exp":               expiresAt.Unix(),
		"iat":               issuedAt.Unix(),
		"iss":               "SenHub",
		"sub":               customerID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		fmt.Printf("❌ Error: Failed to sign license: %v\n", err)
		os.Exit(1)
	}

	// Save license to file
	filename := fmt.Sprintf("license-%s-%s.jwt", customerID, tier)
	err = os.WriteFile(filename, []byte(signedToken), 0644)
	if err != nil {
		fmt.Printf("❌ Error: Failed to save license: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ License generated successfully!")
	fmt.Println()
	fmt.Println("📄 License details:")
	fmt.Printf("   Customer ID: %s\n", customerID)
	fmt.Printf("   Tier: %s\n", tier)
	fmt.Printf("   Authorized probes: %v\n", probes)
	fmt.Printf("   Issued at: %s\n", issuedAt.Format(time.RFC1123))
	fmt.Printf("   Expires at: %s\n", expiresAt.Format(time.RFC1123))
	fmt.Printf("   Duration: %d days\n", validityDays)
	fmt.Println()
	fmt.Printf("💾 Saved to: %s\n", filename)
	fmt.Println()
	fmt.Println("🎫 License token:")
	fmt.Println(signedToken)
	fmt.Println()
	fmt.Println("📋 Customer activation instructions:")
	fmt.Printf("   ./agent license activate %s\n", signedToken)
}
