package cert

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetCertDir(t *testing.T) {
	// Test setting a custom cert directory
	testDir := "/tmp/gocache-test-certs"
	SetCertDir(testDir)

	// Verify it was set
	if certDir != testDir {
		t.Errorf("expected certDir to be %s, got %s", testDir, certDir)
	}
}

func TestGetCertDir(t *testing.T) {
	// Reset to default
	SetCertDir("")

	dir, err := getCertDir()
	if err != nil {
		t.Fatalf("getCertDir failed: %v", err)
	}
	if dir == "" {
		t.Error("getCertDir returned empty string")
	}

	// Verify directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("cert directory was not created")
	}
}

func TestGetCertDirWithCustomPath(t *testing.T) {
	// Test with custom path
	testDir := "/tmp/gocache-test-certs-custom"
	SetCertDir(testDir)

	dir, err := getCertDir()
	if err != nil {
		t.Fatalf("getCertDir failed: %v", err)
	}
	if dir != testDir {
		t.Errorf("expected %s, got %s", testDir, dir)
	}

	// Clean up
	os.RemoveAll(testDir)
}

func TestGenerateCA(t *testing.T) {
	// Test CA generation
	ca, key, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}
	if ca == nil {
		t.Fatal("GenerateCA returned nil certificate")
	}
	if key == nil {
		t.Fatal("GenerateCA returned nil private key")
	}

	// Verify CA properties
	if !ca.IsCA {
		t.Error("generated certificate is not marked as CA")
	}
	if ca.Subject.Organization[0] != "GoCache" {
		t.Errorf("expected organization 'GoCache', got %s", ca.Subject.Organization[0])
	}
	if ca.Subject.CommonName != "GoCache Root CA" {
		t.Errorf("expected common name 'GoCache Root CA', got %s", ca.Subject.CommonName)
	}

	// Verify key size
	if key.Size() != 256 { // 2048 bits = 256 bytes
		t.Errorf("expected key size 256 bytes, got %d", key.Size())
	}
}

func TestSaveCA(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, key, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Save CA
	err = SaveCA(ca, key)
	if err != nil {
		t.Fatalf("SaveCA failed: %v", err)
	}

	// Verify files were created
	caFile := filepath.Join(tmpDir, "ca.crt")
	keyFile := filepath.Join(tmpDir, "ca.key")

	if _, err := os.Stat(caFile); os.IsNotExist(err) {
		t.Error("CA certificate file was not created")
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Error("CA private key file was not created")
	}
}

func TestLoadCA(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Load CA (should generate new one since none exists)
	ca, key, err := LoadCA()
	if err != nil {
		t.Fatalf("LoadCA failed: %v", err)
	}
	if ca == nil {
		t.Fatal("LoadCA returned nil certificate")
	}
	if key == nil {
		t.Fatal("LoadCA returned nil private key")
	}

	// Verify it's a CA
	if !ca.IsCA {
		t.Error("loaded certificate is not marked as CA")
	}
}

func TestLoadCAExisting(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate and save CA
	ca1, key1, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}
	err = SaveCA(ca1, key1)
	if err != nil {
		t.Fatalf("SaveCA failed: %v", err)
	}

	// Load existing CA
	ca2, _, err := LoadCA()
	if err != nil {
		t.Fatalf("LoadCA failed: %v", err)
	}

	// Verify it's the same CA
	if ca1.SerialNumber.Cmp(ca2.SerialNumber) != 0 {
		t.Error("loaded CA has different serial number")
	}
}

func TestGenerateHostCert(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, caKey, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate host certificate
	hostCert, hostKey, err := GenerateHostCert(ca, caKey, "example.com")
	if err != nil {
		t.Fatalf("GenerateHostCert failed: %v", err)
	}
	if hostCert == nil {
		t.Fatal("GenerateHostCert returned nil certificate")
	}
	if hostKey == nil {
		t.Fatal("GenerateHostCert returned nil private key")
	}

	// Verify host certificate properties
	if hostCert.Subject.CommonName != "example.com" {
		t.Errorf("expected common name 'example.com', got %s", hostCert.Subject.CommonName)
	}
	if len(hostCert.DNSNames) != 1 || hostCert.DNSNames[0] != "example.com" {
		t.Errorf("expected DNS name 'example.com', got %v", hostCert.DNSNames)
	}
	if hostCert.IsCA {
		t.Error("host certificate should not be marked as CA")
	}
}

func TestGenerateHostCertIPAddress(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, caKey, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate host certificate for IP address
	hostCert, _, err := GenerateHostCert(ca, caKey, "192.168.1.1")
	if err != nil {
		t.Fatalf("GenerateHostCert failed: %v", err)
	}

	// Verify IP address is in certificate
	if len(hostCert.IPAddresses) != 1 {
		t.Error("expected one IP address in certificate")
	}
	if hostCert.IPAddresses[0].String() != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", hostCert.IPAddresses[0].String())
	}
}

func TestGenerateHostCertWithPort(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, caKey, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate host certificate with port
	hostCert, _, err := GenerateHostCert(ca, caKey, "example.com:8080")
	if err != nil {
		t.Fatalf("GenerateHostCert failed: %v", err)
	}

	// Verify hostname (with port) is in certificate
	if len(hostCert.DNSNames) != 1 || hostCert.DNSNames[0] != "example.com:8080" {
		t.Errorf("expected DNS name 'example.com:8080', got %v", hostCert.DNSNames)
	}
}

func TestSaveCAError(t *testing.T) {
	// Test saving CA to invalid directory
	SetCertDir("/invalid/path/that/does/not/exist")

	ca, key, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// This should fail due to invalid directory
	err = SaveCA(ca, key)
	if err == nil {
		t.Error("expected error for invalid directory")
	}
}

func TestLoadCAError(t *testing.T) {
	// Test loading CA from invalid directory
	SetCertDir("/invalid/path/that/does/not/exist")

	// This should fail due to invalid directory
	_, _, err := LoadCA()
	if err == nil {
		t.Error("expected error for invalid directory")
	}
}

func TestGenerateHostCertEmptyHost(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, caKey, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Test with empty hostname - this should work as the function doesn't validate empty strings
	hostCert, _, err := GenerateHostCert(ca, caKey, "")
	if err != nil {
		t.Errorf("GenerateHostCert with empty hostname failed: %v", err)
	}
	if hostCert == nil {
		t.Error("GenerateHostCert with empty hostname returned nil certificate")
	}
}

func TestCertificateValidityPeriod(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, caKey, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate host certificate
	hostCert, _, err := GenerateHostCert(ca, caKey, "example.com")
	if err != nil {
		t.Fatalf("GenerateHostCert failed: %v", err)
	}

	// Verify validity period
	now := time.Now()
	if hostCert.NotBefore.After(now) {
		t.Error("certificate not before time is in the future")
	}
	if hostCert.NotAfter.Before(now) {
		t.Error("certificate not after time is in the past")
	}

	// Verify CA has longer validity
	caDuration := ca.NotAfter.Sub(ca.NotBefore)
	hostDuration := hostCert.NotAfter.Sub(hostCert.NotBefore)
	if caDuration <= hostDuration {
		t.Error("CA should have longer validity period than host certificate")
	}
}

func TestCertificateKeyUsage(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "gocache-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set cert directory
	SetCertDir(tmpDir)

	// Generate CA
	ca, caKey, err := GenerateCA()
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	// Generate host certificate
	hostCert, _, err := GenerateHostCert(ca, caKey, "example.com")
	if err != nil {
		t.Fatalf("GenerateHostCert failed: %v", err)
	}

	// Verify CA key usage
	if ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA should have cert sign key usage")
	}

	// Verify host cert key usage
	if hostCert.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		t.Error("host certificate should have key encipherment usage")
	}
	if hostCert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("host certificate should have digital signature usage")
	}

	// Verify host cert extended key usage
	if len(hostCert.ExtKeyUsage) != 1 || hostCert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Error("host certificate should have server auth extended key usage")
	}
}
