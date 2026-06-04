package setup

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var nodejsCmd = &cobra.Command{
	Use:   "nodejs",
	Short: "Install Node.js via nvm",
	RunE:  runNodeJS,
}

func init() {
	SetupCmd.AddCommand(nodejsCmd)
}

func runNodeJS(_ *cobra.Command, _ []string) error {
	// Check if node is already available
	if path, err := exec.LookPath("node"); err == nil {
		out, _ := exec.Command("node", "--version").Output()
		ui.Info(fmt.Sprintf("Node.js already installed at %s: %s", path, strings.TrimSpace(string(out))))
		return nil
	}

	ui.Header("Installing Node.js")

	if runtime.GOOS == "darwin" {
		return installNodeMac()
	}
	return installNodeLinux()
}

func installNodeLinux() error {
	ui.Info("Installing Node.js via NodeSource repository...")
	script := `curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash -`
	cmd := exec.Command("bash", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("NodeSource setup: %w", err)
	}
	if err := system.Run("apt-get", "install", "-y", "nodejs"); err != nil {
		return fmt.Errorf("installing nodejs: %w", err)
	}
	out, _ := exec.Command("node", "--version").Output()
	ui.Success("Node.js installed: " + strings.TrimSpace(string(out)))
	return nil
}

func installNodeMac() error {
	ui.Info("Installing Node.js via Homebrew...")
	if err := system.Run("brew", "install", "node"); err != nil {
		return fmt.Errorf("brew install node: %w", err)
	}
	out, _ := exec.Command("node", "--version").Output()
	ui.Success("Node.js installed: " + strings.TrimSpace(string(out)))
	return nil
}
