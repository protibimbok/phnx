package phpcmd

import (
	"fmt"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <version>",
	Short: "Uninstall a PHP version",
	Args:  cobra.ExactArgs(1),
	RunE:  runUninstall,
}

func init() {
	PHPCmd.AddCommand(uninstallCmd)
}

func runUninstall(_ *cobra.Command, args []string) error {
	version := args[0]
	if err := php.ValidateVersion(version); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Check no sites use this version
	var usingSites []string
	for _, s := range cfg.Sites {
		if s.PHP == version {
			usingSites = append(usingSites, s.Subdomain)
		}
	}
	if len(usingSites) > 0 {
		ui.Error(fmt.Sprintf("PHP %s is used by the following sites:", version))
		for _, s := range usingSites {
			fmt.Printf("  - %s\n", s)
		}
		return fmt.Errorf("unpin or remove those sites before uninstalling PHP %s", version)
	}

	ok, err := ui.Confirm(fmt.Sprintf("Uninstall PHP %s?", version), false)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// Stop and disable FPM service
	resolved, _ := php.ResolvePHP(cfg, version)
	svc := system.NewServiceManager()
	_ = svc.Stop(resolved.Service)
	_ = svc.Disable(resolved.Service)

	// Remove packages
	if err := php.Uninstall(version); err != nil {
		ui.Warn(fmt.Sprintf("package removal: %v", err))
	}

	// Remove pool config
	if err := fpm.RemovePool(version); err != nil {
		ui.Warn(fmt.Sprintf("removing pool config: %v", err))
	}

	// Update config
	cfg.RemovePHPVersion(version)
	if cfg.DefaultPHP == version {
		cfg.DefaultPHP = ""
		ui.Warn("This was the default PHP version — run 'phnx php default <version>' to set a new one")
	}
	if err := config.Save(cfg); err != nil {
		return err
	}

	ui.Success(fmt.Sprintf("PHP %s uninstalled", version))
	return nil
}
