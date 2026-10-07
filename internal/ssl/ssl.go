// Package ssl generates a local certificate authority and per-site TLS
// certificates so phnx sites can be served over https://. Everything is done
// with Go's crypto/x509 — no openssl or mkcert dependency.
//
// Layout under ~/.phnx/ssl:
//
//	ca/rootCA.pem          CA certificate (public, installed into trust stores)
//	ca/rootCA-key.pem      CA private key (0600)
//	certs/<name>.pem       per-site certificate
//	certs/<name>-key.pem   per-site private key
package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/protibimbok/phnx/internal/config"
)

const (
	caName       = "phnx local CA"
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 825 * 24 * time.Hour // Apple rejects leaf certs valid for longer
)

// Dir returns the root of phnx's SSL state directory.
func Dir() string { return filepath.Join(config.Dir(), "ssl") }

// CACertPath returns the path of the CA certificate (PEM).
func CACertPath() string { return filepath.Join(Dir(), "ca", "rootCA.pem") }

// CAKeyPath returns the path of the CA private key (PEM).
func CAKeyPath() string { return filepath.Join(Dir(), "ca", "rootCA-key.pem") }

// CertPath returns the certificate path for a site.
func CertPath(subdomain string) string {
	return filepath.Join(Dir(), "certs", subdomain+".pem")
}

// KeyPath returns the private key path for a site.
func KeyPath(subdomain string) string {
	return filepath.Join(Dir(), "certs", subdomain+"-key.pem")
}

// CAExists reports whether both CA files are present.
func CAExists() bool {
	_, errCert := os.Stat(CACertPath())
	_, errKey := os.Stat(CAKeyPath())
	return errCert == nil && errKey == nil
}

// EnsureCA creates the local CA if it does not exist yet.
// It returns true when a new CA was generated.
func EnsureCA() (created bool, err error) {
	if CAExists() {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(CACertPath()), 0o700); err != nil {
		return false, fmt.Errorf("creating CA directory: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, fmt.Errorf("generating CA key: %w", err)
	}

	hostname, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber: newSerial(),
		Subject: pkix.Name{
			CommonName:         caName,
			Organization:       []string{"phnx development CA"},
			OrganizationalUnit: []string{hostname},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return false, fmt.Errorf("creating CA certificate: %w", err)
	}

	if err := writeKey(CAKeyPath(), key); err != nil {
		return false, err
	}
	if err := writeCert(CACertPath(), der); err != nil {
		return false, err
	}
	return true, nil
}

// IssueCert creates (or re-creates) a certificate for domain, signed by the
// local CA. The cert covers both the domain and *.domain so nested
// subdomains (e.g. api.myapp.test) work too.
func IssueCert(subdomain, domain string) error {
	caCert, caKey, err := loadCA()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(CertPath(subdomain)), 0o700); err != nil {
		return fmt.Errorf("creating certs directory: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating site key: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: newSerial(),
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"phnx development certificate"},
		},
		DNSNames:    []string{domain, "*." + domain},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(leafValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("signing site certificate: %w", err)
	}

	if err := writeKey(KeyPath(subdomain), key); err != nil {
		return err
	}
	return writeCert(CertPath(subdomain), der)
}

// RemoveCert deletes a site's certificate and key (missing files are not an error).
func RemoveCert(subdomain string) error {
	var errs []error
	for _, p := range []string{CertPath(subdomain), KeyPath(subdomain)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CertExists reports whether a site has both a certificate and a key on disk.
func CertExists(subdomain string) bool {
	_, errCert := os.Stat(CertPath(subdomain))
	_, errKey := os.Stat(KeyPath(subdomain))
	return errCert == nil && errKey == nil
}

// CertExpiry returns the NotAfter time of a site's certificate.
func CertExpiry(subdomain string) (time.Time, error) {
	cert, err := readCert(CertPath(subdomain))
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

func loadCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	if !CAExists() {
		return nil, nil, fmt.Errorf("local CA not found — run 'phnx secure' to create it")
	}
	cert, err := readCert(CACertPath())
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(CAKeyPath())
	if err != nil {
		return nil, nil, fmt.Errorf("reading CA key: %w", err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("CA key %s is not valid PEM", CAKeyPath())
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing CA key: %w", err)
	}
	return cert, key, nil
}

func readCert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s is not valid PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cert, nil
}

func writeCert(path string, der []byte) error {
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("encoding private key: %w", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func newSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return serial
}
