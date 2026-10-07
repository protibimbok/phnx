package ssl

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
)

const (
	anchorFile  = "phnx-root-ca.crt"
	nssNickname = "phnx local CA"
)

// TrustStore describes where the OS keeps user-added root certificates.
type TrustStore struct {
	AnchorPath string   // where the CA PEM is copied
	Update     []string // command that rebuilds the trust bundle (empty on macOS)
}

// systemTrustStore picks the trust store for the current OS/distro.
// Returns nil when no known store is found.
func systemTrustStore() *TrustStore {
	if runtime.GOOS == "darwin" {
		return &TrustStore{AnchorPath: "/Library/Keychains/System.keychain"}
	}

	type candidate struct {
		dir    string
		update []string
	}
	candidates := []candidate{
		{"/etc/ca-certificates/trust-source/anchors", []string{"update-ca-trust"}}, // Arch
		{"/etc/pki/ca-trust/source/anchors", []string{"update-ca-trust"}},          // Fedora / RHEL
		{"/usr/local/share/ca-certificates", []string{"update-ca-certificates"}},   // Debian / Ubuntu / Alpine
	}

	// Prefer the distro-canonical location, then fall back to whatever exists.
	switch {
	case system.IsArch():
		candidates = append([]candidate{candidates[0]}, candidates...)
	case system.IsFedora():
		candidates = append([]candidate{candidates[1]}, candidates...)
	case system.IsDebian():
		candidates = append([]candidate{candidates[2]}, candidates...)
	}
	for _, c := range candidates {
		if info, err := os.Stat(c.dir); err == nil && info.IsDir() {
			if _, err := exec.LookPath(c.update[0]); err == nil {
				return &TrustStore{AnchorPath: filepath.Join(c.dir, anchorFile), Update: c.update}
			}
		}
	}
	return nil
}

// IsTrusted reports whether the current CA certificate is installed in the
// system trust store. On macOS it queries the keychain; on Linux it compares
// the installed anchor file against the CA PEM.
func IsTrusted() bool {
	caPEM, err := os.ReadFile(CACertPath())
	if err != nil {
		return false
	}
	if runtime.GOOS == "darwin" {
		out, err := system.OutputUser("security", "find-certificate", "-c", caName, "-p", "/Library/Keychains/System.keychain")
		if err != nil {
			return false
		}
		return strings.Contains(out, strings.TrimSpace(string(caPEM)))
	}
	store := systemTrustStore()
	if store == nil {
		return false
	}
	installed, err := os.ReadFile(store.AnchorPath)
	if err != nil {
		return false
	}
	return bytes.Equal(bytes.TrimSpace(installed), bytes.TrimSpace(caPEM))
}

// InstallTrust copies the CA certificate into the system trust store and
// rebuilds the bundle. This needs sudo on Linux and macOS.
func InstallTrust() error {
	caPEM, err := os.ReadFile(CACertPath())
	if err != nil {
		return fmt.Errorf("reading CA certificate: %w", err)
	}

	if runtime.GOOS == "darwin" {
		return system.Run("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", CACertPath())
	}

	store := systemTrustStore()
	if store == nil {
		return fmt.Errorf("no supported system trust store found — install the CA manually: %s", CACertPath())
	}
	if err := system.WriteFile(store.AnchorPath, string(caPEM)); err != nil {
		return err
	}
	return system.Run(store.Update...)
}

// UninstallTrust removes the CA from the system trust store.
func UninstallTrust() error {
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(CACertPath()); err != nil {
			return nil
		}
		return system.Run("security", "remove-trusted-cert", "-d", CACertPath())
	}
	store := systemTrustStore()
	if store == nil {
		return nil
	}
	if _, err := os.Stat(store.AnchorPath); err != nil {
		return nil
	}
	if err := system.RemoveFile(store.AnchorPath); err != nil {
		return err
	}
	return system.Run(store.Update...)
}

// InstallNSSTrust adds the CA to the NSS databases used by Firefox and by
// Chrome/Chromium on Linux (which ignore the system bundle). It is best-effort:
// it returns the list of databases updated and a non-fatal error if certutil
// is missing.
func InstallNSSTrust() (updated []string, err error) {
	if runtime.GOOS == "darwin" {
		return nil, nil // Safari/Chrome use the keychain; Firefox honours enterprise roots by default
	}
	certutil, lookErr := exec.LookPath("certutil")
	if lookErr != nil {
		return nil, fmt.Errorf("certutil not found (install the nss / libnss3-tools package) — Firefox and Chrome will not trust the CA until it is added manually")
	}

	for _, db := range nssDatabases() {
		if err := nssAdd(certutil, db); err == nil {
			updated = append(updated, db)
		}
	}
	return updated, nil
}

// UninstallNSSTrust removes the CA from all NSS databases it was added to.
func UninstallNSSTrust() {
	certutil, err := exec.LookPath("certutil")
	if err != nil {
		return
	}
	for _, db := range nssDatabases() {
		_, _ = system.OutputUser(certutil, "-d", "sql:"+db, "-D", "-n", nssNickname)
	}
}

func nssAdd(certutil, db string) error {
	if _, err := os.Stat(filepath.Join(db, "cert9.db")); err != nil {
		// Chrome's shared DB may not exist yet — create it. Firefox profile DBs
		// are only touched if they already exist.
		if !strings.HasSuffix(db, ".pki/nssdb") {
			return err
		}
		if err := os.MkdirAll(db, 0o700); err != nil {
			return err
		}
		if _, err := system.OutputUser(certutil, "-d", "sql:"+db, "-N", "--empty-password"); err != nil {
			return err
		}
	}
	// Replace any previous phnx CA so a regenerated CA takes effect.
	_, _ = system.OutputUser(certutil, "-d", "sql:"+db, "-D", "-n", nssNickname)
	_, err := system.OutputUser(certutil, "-d", "sql:"+db, "-A", "-t", "C,,", "-n", nssNickname, "-i", CACertPath())
	return err
}

// nssDatabases lists NSS DB directories for the current user: Chrome's shared
// DB plus every Firefox profile (native, snap, and flatpak installs).
func nssDatabases() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dbs := []string{filepath.Join(home, ".pki", "nssdb")}
	globs := []string{
		filepath.Join(home, ".mozilla", "firefox", "*"),
		filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "*"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox", "*"),
		filepath.Join(home, ".librewolf", "*"),
	}
	for _, g := range globs {
		matches, _ := filepath.Glob(g)
		for _, m := range matches {
			if _, err := os.Stat(filepath.Join(m, "cert9.db")); err == nil {
				dbs = append(dbs, m)
			}
		}
	}
	return dbs
}

// IsWSL reports whether phnx is running inside Windows Subsystem for Linux.
func IsWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

// InstallWindowsTrust imports the CA into the Windows user "Trusted Root"
// store from inside WSL so Windows browsers trust phnx sites. Windows shows a
// confirmation dialog. Returns an error if certutil.exe or wslpath is missing.
func InstallWindowsTrust() error {
	certutil, err := exec.LookPath("certutil.exe")
	if err != nil {
		return fmt.Errorf("certutil.exe not found in PATH — import %s into the Windows 'Trusted Root Certification Authorities' store manually", CACertPath())
	}
	winPath, err := system.OutputUser("wslpath", "-w", CACertPath())
	if err != nil {
		return fmt.Errorf("converting path for Windows: %w", err)
	}
	_, err = system.OutputUser(certutil, "-user", "-addstore", "Root", strings.TrimSpace(winPath))
	return err
}
