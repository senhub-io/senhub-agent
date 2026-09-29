package http

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDescribeTLSReadsTheCertificate(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	notAfter := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "agent.example.com"},
		NotBefore: notAfter.AddDate(-1, 0, 0), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	v := describeTLS(true, path, "/k.pem", "1.2", notAfter.AddDate(0, -1, 0))
	if !v.Enabled || v.Subject != "CN=agent.example.com" || v.NotAfter != "2027-03-01T00:00:00Z" || v.Expired || v.CertError != "" {
		t.Errorf("view = %+v", v)
	}
	if v := describeTLS(true, path, "", "", notAfter.AddDate(0, 0, 1)); !v.Expired {
		t.Error("a certificate past its NotAfter is not reported expired")
	}
	if v := describeTLS(true, filepath.Join(t.TempDir(), "missing.pem"), "", "", time.Now()); v.CertError == "" {
		t.Error("an unreadable certificate file is not reported")
	}
}
