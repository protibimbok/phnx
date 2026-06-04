package setup

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var composerForce bool

var composerCmd = &cobra.Command{
	Use:   "composer",
	Short: "Install Composer globally",
	RunE:  runComposer,
}

func init() {
	SetupCmd.AddCommand(composerCmd)
	composerCmd.Flags().BoolVar(&composerForce, "force", false, "Re-install even if composer already exists")
}

func runComposer(_ *cobra.Command, _ []string) error {
	dest := "/usr/local/bin/composer"

	if !composerForce {
		if _, err := os.Stat(dest); err == nil {
			ui.Info("Composer is already installed at " + dest)
			out, _ := exec.Command("composer", "--version").Output()
			fmt.Print(string(out))
			return nil
		}
	}

	ui.Info("Downloading Composer installer...")

	// Download installer
	installer, err := download("https://getcomposer.org/installer")
	if err != nil {
		return fmt.Errorf("downloading composer installer: %w", err)
	}

	// Verify checksum
	sig, err := download("https://composer.github.io/installer.sig")
	if err != nil {
		return fmt.Errorf("downloading installer signature: %w", err)
	}
	expected := strings.TrimSpace(string(sig))
	actual := sha384sum(installer)
	if actual != expected {
		return fmt.Errorf("installer checksum mismatch (got %s, want %s)", actual, expected)
	}
	ui.Success("Checksum verified")

	// Write installer to temp file
	tmp, err := os.CreateTemp("", "composer-installer-*.php")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(installer); err != nil {
		return err
	}
	tmp.Close()

	// Run installer
	ui.Info("Installing Composer to " + dest + "...")
	cmd := exec.Command("php", tmp.Name(), "--install-dir=/usr/local/bin", "--filename=composer")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Retry with sudo
		if err2 := system.Run("php", tmp.Name(), "--install-dir=/usr/local/bin", "--filename=composer"); err2 != nil {
			return fmt.Errorf("installing composer: %w", err2)
		}
	}

	out, _ := exec.Command("composer", "--version").Output()
	ui.Success("Composer installed: " + strings.TrimSpace(string(out)))
	return nil
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func sha384sum(data []byte) string {
	h := sha512.New384()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
