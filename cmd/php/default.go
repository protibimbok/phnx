package phpcmd

import (
	"fmt"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var defaultCmd = &cobra.Command{
	Use:   "default <version>",
	Short: "Set the default PHP version",
	Args:  cobra.ExactArgs(1),
	RunE:  runDefault,
}

func init() {
	PHPCmd.AddCommand(defaultCmd)
}

func runDefault(_ *cobra.Command, args []string) error {
	version := args[0]
	if err := php.ValidateVersion(version); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if !cfg.HasPHPVersion(version) {
		return fmt.Errorf("PHP %s is not installed — run 'phnx php install %s' first", version, version)
	}

	cfg.DefaultPHP = version
	if err := config.Save(cfg); err != nil {
		return err
	}

	// Update /usr/local/bin/php symlink
	resolved, err := php.ResolvePHP(cfg, version)
	if err != nil {
		return err
	}
	if err := system.Run("ln", "-sf", resolved.Binary, "/usr/local/bin/php"); err != nil {
		ui.Warn(fmt.Sprintf("could not update /usr/local/bin/php symlink: %v", err))
	}

	ui.Success(fmt.Sprintf("Default PHP set to %s", version))
	return nil
}
