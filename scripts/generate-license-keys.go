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

// DEVELOPMENT ONLY - LICENSE KEY GENERATOR FOR TESTING
//
// ⚠️ WARNING: This script generates TEST keys for development and testing only.
//
// DO NOT USE THESE KEYS IN PRODUCTION!
//
// For production license generation, use:
//   scripts/sensor-factory-license-generator.go
//
// This script will:
// 1. Generate test RSA key pair (4096-bit)
// 2. Create example JWT licenses for testing
//
// Output files:
//   - license-private-key.pem (TEST ONLY - delete before deployment)
//   - license-public-key.pem
//   - example-license-pro.jwt
//   - example-license-enterprise.jwt
//   - example-license-grace-period.jwt

func main() {
	fmt.Println("⚠️  DEVELOPMENT LICENSE KEY GENERATOR - TEST ONLY")
	fmt.Println("==================================================")
	fmt.Println()
	fmt.Println("This script generates TEST keys for development.")
	fmt.Println("DO NOT use these keys in production!")
	fmt.Println()
	fmt.Println("For production, use: sensor-factory-license-generator.go")
	fmt.Println()
	fmt.Println("🔑 Generating RSA key pair for license system...")

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

	// Save private key (for Sensor Factory)
	err = os.WriteFile("license-private-key.pem", privateKeyPEM, 0600)
	if err != nil {
		panic(err)
	}

	// Save public key (for agent)
	err = os.WriteFile("license-public-key.pem", publicKeyPEM, 0644)
	if err != nil {
		panic(err)
	}

	fmt.Println("✅ Test keys generated:")
	fmt.Println("   Private key: license-private-key.pem (TEST ONLY)")
	fmt.Println("   Public key:  license-public-key.pem")

	// Generate example JWT licenses
	fmt.Println("\n🎫 Generating example JWT licenses...")

	generateExampleLicense(privateKey, "pro", []string{"redfish", "citrix"}, "test-customer-pro")
	generateExampleLicense(privateKey, "enterprise", []string{"*"}, "test-customer-enterprise")
	generateExampleLicenseExpired(privateKey, "pro", []string{"redfish"}, "test-customer-grace")

	fmt.Println("\n✅ Done!")
	fmt.Println()
	fmt.Println("⚠️  REMINDER: These are TEST keys for development only!")
	fmt.Println("   - Delete license-private-key.pem before deploying agent")
	fmt.Println("   - Replace public key in public_key.go with production key")
	fmt.Println("   - Use sensor-factory-license-generator.go for production licenses")
	fmt.Println()
	fmt.Println("📚 Documentation: /docs/LICENSE-SYSTEM.md")
}

func generateExampleLicense(privateKey *rsa.PrivateKey, tier string, probes []string, subject string) {
	claims := jwt.MapClaims{
		"tier":              tier,
		"authorized_probes": probes,
		"exp":               time.Now().Add(365 * 24 * time.Hour).Unix(), // 1 year
		"iat":               time.Now().Unix(),
		"iss":               "SenHub",
		"sub":               subject,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		panic(err)
	}

	filename := fmt.Sprintf("example-license-%s.jwt", tier)
	err = os.WriteFile(filename, []byte(signedToken), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   %s: %s\n", tier, filename)
}

func generateExampleLicenseExpired(privateKey *rsa.PrivateKey, tier string, probes []string, subject string) {
	claims := jwt.MapClaims{
		"tier":              tier,
		"authorized_probes": probes,
		"exp":               time.Now().Add(-10 * 24 * time.Hour).Add(3 * 24 * time.Hour).Unix(), // Expired 3 days ago (grace period)
		"iat":               time.Now().Add(-365 * 24 * time.Hour).Unix(),
		"iss":               "SenHub",
		"sub":               subject,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(privateKey)
	if err != nil {
		panic(err)
	}

	filename := "example-license-grace-period.jwt"
	err = os.WriteFile(filename, []byte(signedToken), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   grace-period: %s\n", filename)
}
