package setup

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var wpcliCmd = &cobra.Command{
	Use:   "wpcli",
	Short: "Install WP-CLI globally",
	RunE:  runWPCLI,
}

func init() {
	SetupCmd.AddCommand(wpcliCmd)
}

func runWPCLI(_ *cobra.Command, _ []string) error {
	dest := "/usr/local/bin/wp"

	if _, err := os.Stat(dest); err == nil {
		ui.Info("WP-CLI is already installed at " + dest)
		out, _ := exec.Command("wp", "--info").Output()
		fmt.Print(string(out))
		return nil
	}

	ui.Info("Downloading WP-CLI...")

	phar, err := download("https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar")
	if err != nil {
		return fmt.Errorf("downloading wp-cli.phar: %w", err)
	}

	sigData, err := download("https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar.sha512")
	if err != nil {
		return fmt.Errorf("downloading wp-cli checksum: %w", err)
	}
	expected := strings.Fields(strings.TrimSpace(string(sigData)))[0]
	h := sha512.New()
	h.Write(phar)
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("wp-cli checksum mismatch")
	}
	ui.Success("Checksum verified")

	// Write to temp file
	tmp, err := os.CreateTemp("", "wp-cli-*.phar")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(phar); err != nil {
		return err
	}
	tmp.Close()

	// chmod +x and move to /usr/local/bin/wp
	if err := system.Run("chmod", "+x", tmp.Name()); err != nil {
		return err
	}
	if err := system.Run("mv", tmp.Name(), dest); err != nil {
		return err
	}

	out, _ := exec.Command("wp", "--info").Output()
	ui.Success("WP-CLI installed:\n" + string(out))
	return nil
}
