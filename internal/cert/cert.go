package cert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
)

var certDir string

// SetCertDir sets the directory where certificates are stored.
func SetCertDir(dir string) {
	certDir = dir
}

// getCertDir returns the directory where certificates are stored.
func getCertDir() (string, error) {
	if certDir != "" {
		return certDir, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	certDir := filepath.Join(configDir, "gocache", "certs")
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return "", err
	}
	return certDir, nil
}

// LoadCA loads the CA certificate and private key from disk.
func LoadCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	certDir, err := getCertDir()
	if err != nil {
		return nil, nil, err
	}

	certPath := filepath.Join(certDir, caCertFile)
	keyPath := filepath.Join(certDir, caKeyFile)

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		ca, key, err := GenerateCA()
		if err != nil {
			return nil, nil, err
		}
		if err := SaveCA(ca, key); err != nil {
			return nil, nil, err
		}
		return ca, key, nil
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}

	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)

	ca, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	return ca, key, nil
}

// SaveCA saves the CA certificate and private key to disk.
func SaveCA(ca *x509.Certificate, key *rsa.PrivateKey) error {
	certDir, err := getCertDir()
	if err != nil {
		return err
	}

	certPath := filepath.Join(certDir, caCertFile)
	keyPath := filepath.Join(certDir, caKeyFile)

	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
	certOut.Close()

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	keyOut.Close()

	return nil
}

// GenerateCA creates a new root Certificate Authority certificate and private key.
func GenerateCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"GoCache"},
			CommonName:   "GoCache Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}

	ca, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, nil, err
	}

	return ca, priv, nil
}

// GenerateHostCert creates a new host certificate signed by the provided CA.
func GenerateHostCert(ca *x509.Certificate, caPriv *rsa.PrivateKey, host string) (*x509.Certificate, *rsa.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			CommonName: host,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Handle hosts that are IP addresses.
	if ip := net.ParseIP(strings.Split(host, ":")[0]); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, ca, &priv.PublicKey, caPriv)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return nil, nil, err
	}

	return cert, priv, nil
}