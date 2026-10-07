package ssl

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestIssueCertVerifiesAgainstCA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	created, err := EnsureCA()
	if err != nil || !created {
		t.Fatalf("EnsureCA: created=%v err=%v", created, err)
	}
	if created, err := EnsureCA(); err != nil || created {
		t.Fatalf("second EnsureCA should be a no-op: created=%v err=%v", created, err)
	}
	if info, _ := os.Stat(CAKeyPath()); info.Mode().Perm() != 0o600 {
		t.Fatalf("CA key perms = %o, want 600", info.Mode().Perm())
	}

	if err := IssueCert("myapp", "myapp.test"); err != nil {
		t.Fatal(err)
	}
	if !CertExists("myapp") {
		t.Fatal("cert files missing")
	}

	ca, err := readCert(CACertPath())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := readCert(CertPath("myapp"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, name := range []string{"myapp.test", "api.myapp.test"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name}); err != nil {
			t.Errorf("verify %s: %v", name, err)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "other.test"}); err == nil {
		t.Error("other.test should not verify")
	}

	if err := RemoveCert("myapp"); err != nil {
		t.Fatal(err)
	}
	if CertExists("myapp") {
		t.Fatal("cert should be removed")
	}
	if err := RemoveCert("myapp"); err != nil {
		t.Fatalf("removing a missing cert should not error: %v", err)
	}
	if filepath.Dir(CertPath("x")) != filepath.Join(home, ".phnx", "ssl", "certs") {
		t.Fatalf("unexpected cert dir %s", CertPath("x"))
	}
}
